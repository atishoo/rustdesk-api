package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

var (
	ErrDeviceCredentialDisabled  = errors.New("device credential storage is disabled")
	ErrDeviceCredentialForbidden = errors.New("device credential access denied")
	ErrDeviceCredentialNotFound  = errors.New("device credential not found")
	ErrDeviceCredentialInvalid   = errors.New("invalid device credential")
)

type DeviceCredential struct {
	PasswordHash string `json:"password_hash"`
	Salt         string `json:"salt"`
	PeerKey      string `json:"peer_key"`
}

type storedDeviceCredential struct {
	DeviceCredential
	ValidUntil int64 `json:"valid_until"`
}

func validDeviceCredentialValue(value string) bool {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == value
}

func (credential DeviceCredential) Valid() bool {
	return len(credential.Salt) > 0 && len(credential.Salt) <= 256 &&
		validDeviceCredentialValue(credential.PasswordHash) && validDeviceCredentialValue(credential.PeerKey)
}

func deviceCredentialCipher() (cipher.AEAD, error) {
	key, err := Config.Capylink.DeviceCredentialKeyBytes()
	if err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return nil, ErrDeviceCredentialDisabled
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func deviceCredentialAAD(session *model.UserToken) []byte {
	return []byte(fmt.Sprintf("capylink-device-credential-v1\x00%d\x00%d\x00%d:%s%d:%s", session.Id,
		session.UserId, len(session.DeviceId), session.DeviceId, len(session.DeviceUuid), session.DeviceUuid))
}

func deviceCredentialSession(token string) (*model.UserToken, error) {
	user, session := AllService.UserService.InfoByAccessToken(token)
	if user.Id == 0 || user.Status != model.COMMON_STATUS_ENABLE || session.DeviceId == "" || session.DeviceUuid == "" {
		return nil, ErrDeviceCredentialForbidden
	}
	var count int64
	if err := DB.Model(&model.Peer{}).Where("id = ? AND uuid = ? AND user_id = ? AND identity_confirmed = TRUE",
		session.DeviceId, session.DeviceUuid, session.UserId).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrDeviceCredentialForbidden
	}
	return session, nil
}

func (ps *PeerService) StoreDeviceCredential(token string, id, uuid model.DeviceIdentity, credential DeviceCredential) error {
	aead, err := deviceCredentialCipher()
	if err != nil {
		return err
	}
	if !credential.Valid() {
		return ErrDeviceCredentialInvalid
	}
	session, err := deviceCredentialSession(token)
	if err != nil {
		return err
	}
	if id != session.DeviceId || uuid != session.DeviceUuid {
		return ErrDeviceCredentialForbidden
	}
	plaintext, err := json.Marshal(storedDeviceCredential{DeviceCredential: credential, ValidUntil: time.Now().Unix() + 90})
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	encrypted := aead.Seal(nonce, nonce, plaintext, deviceCredentialAAD(session))
	return updateDeviceCredential(session, base64.StdEncoding.EncodeToString(encrypted))
}

func (ps *PeerService) ClearDeviceCredential(token string, id, uuid model.DeviceIdentity) error {
	if _, err := deviceCredentialCipher(); err != nil {
		return err
	}
	session, err := deviceCredentialSession(token)
	if err != nil {
		return err
	}
	if id != session.DeviceId || uuid != session.DeviceUuid {
		return ErrDeviceCredentialForbidden
	}
	return updateDeviceCredential(session, "")
}

func updateDeviceCredential(session *model.UserToken, ciphertext string) error {
	result := DB.Model(&model.UserToken{}).
		Where("id = ? AND token = ? AND user_id = ? AND device_id = ? AND device_uuid = ? AND expired_at >= ?",
			session.Id, session.Token, session.UserId, session.DeviceId, session.DeviceUuid, time.Now().Unix()).
		Where("EXISTS (SELECT 1 FROM peers WHERE peers.id = ? AND peers.uuid = ? AND peers.user_id = ? AND peers.identity_confirmed = TRUE)",
			session.DeviceId, session.DeviceUuid, session.UserId).
		UpdateColumn("device_credential", ciphertext)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 && ciphertext != "" {
		return ErrDeviceCredentialForbidden
	}
	return nil
}

func (ps *PeerService) ResolveDeviceCredential(token string, id model.DeviceIdentity, salt, peerKey string) (*DeviceCredential, error) {
	aead, err := deviceCredentialCipher()
	if err != nil {
		return nil, err
	}
	if id == "" || len(id) > 256 || salt == "" || len(salt) > 256 || !validDeviceCredentialValue(peerKey) {
		return nil, ErrDeviceCredentialInvalid
	}
	caller, err := deviceCredentialSession(token)
	if err != nil {
		return nil, err
	}
	var peers []model.Peer
	if err := DB.Where("id = ? AND identity_confirmed = TRUE", id).Limit(2).Find(&peers).Error; err != nil {
		return nil, err
	}
	if len(peers) != 1 {
		return nil, ErrDeviceCredentialNotFound
	}
	peer := peers[0]
	if peer.UserId != caller.UserId {
		return nil, ErrDeviceCredentialForbidden
	}
	var sessions []model.UserToken
	if err := DB.Where("user_id = ? AND device_id = ? AND device_uuid = ? AND expired_at >= ? AND device_credential <> ''",
		caller.UserId, peer.Id, peer.Uuid, time.Now().Unix()).Order("id DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if len(Jwt.Key) > 0 {
			if userID, err := AllService.UserService.VerifyJWT(session.Token); err != nil || userID != session.UserId {
				continue
			}
		}
		encrypted, err := base64.StdEncoding.Strict().DecodeString(session.DeviceCredential)
		if err != nil || len(encrypted) < aead.NonceSize()+aead.Overhead() {
			continue
		}
		plaintext, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], deviceCredentialAAD(&session))
		if err != nil {
			continue
		}
		var credential storedDeviceCredential
		if err := json.Unmarshal(plaintext, &credential); err != nil || !credential.Valid() || credential.ValidUntil <= time.Now().Unix() || credential.Salt != salt || credential.PeerKey != peerKey {
			continue
		}
		// Recheck both live sessions and device ownership after decrypting; revoked sessions are not credential grants.
		if _, err := deviceCredentialSession(token); err != nil {
			return nil, err
		}
		var count int64
		if err := DB.Model(&model.UserToken{}).Where("user_tokens.id = ? AND user_tokens.token = ? AND user_tokens.expired_at >= ? AND user_tokens.device_credential = ?",
			session.Id, session.Token, time.Now().Unix(), session.DeviceCredential).
			Where("EXISTS (SELECT 1 FROM peers WHERE peers.id = ? AND peers.uuid = ? AND peers.user_id = ? AND peers.identity_confirmed = TRUE)", peer.Id, peer.Uuid, caller.UserId).
			Where("NOT EXISTS (SELECT 1 FROM peers AS other_peer WHERE other_peer.id = ? AND other_peer.uuid <> ? AND other_peer.identity_confirmed = TRUE)", peer.Id, peer.Uuid).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, ErrDeviceCredentialNotFound
		}
		return &credential.DeviceCredential, nil
	}
	return nil, ErrDeviceCredentialNotFound
}

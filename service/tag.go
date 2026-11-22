package service

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
)

type TagService struct {
}

var (
	ErrInvalidAddressBookTags = errors.New("InvalidAddressBookTags")
	ErrReferencedTagMove      = errors.New("ReferencedTagMove")
)

func tagsForAccount(tx *gorm.DB, userID uint) *gorm.DB {
	return tx.Model(&model.Tag{}).Where("user_id = ?", userID)
}

func decodeAddressBookTags(encoded []byte) ([]string, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	var values []*string
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, ErrInvalidAddressBookTags
	}
	var tags []string
	for _, value := range values {
		if value == nil {
			return nil, ErrInvalidAddressBookTags
		}
		tags = append(tags, *value)
	}
	return tags, nil
}

func validateAccountTags(tx *gorm.DB, userID uint, encoded []byte) error {
	tags, err := decodeAddressBookTags(encoded)
	if err != nil || len(tags) == 0 {
		return err
	}
	var available []string
	if err := tagsForAccount(tx, userID).Where("name IN ?", tags).Pluck("name", &available).Error; err != nil {
		return err
	}
	for _, name := range tags {
		if !slices.Contains(available, name) {
			return ErrInvalidAddressBookTags
		}
	}
	return nil
}

func (s *TagService) Info(id uint) *model.Tag {
	p := &model.Tag{}
	DB.Where("id = ?", id).First(p)
	return p
}
func (s *TagService) InfoByUserIdAndName(userid uint, name string) *model.Tag {
	var candidates []model.Tag
	tagsForAccount(DB, userid).Where("name = ?", name).Order("id asc").Find(&candidates)
	for _, tag := range candidates {
		if tag.Name == name {
			return &tag
		}
	}
	return &model.Tag{}
}

func (s *TagService) ListByUserId(userId uint) (res *model.TagList) {
	res = s.List(1, 1000, func(tx *gorm.DB) {
		tx.Where("user_id = ?", userId)
		tx.Order("name asc")
	})
	return
}
func updatePersonalAddressBookTags(tx *gorm.DB, userId uint, pending map[string]struct{}, colors map[string]uint) error {
	var current []*model.Tag
	if err := tagsForAccount(tx, userId).Find(&current).Error; err != nil {
		return err
	}
	for _, tag := range current {
		if color, ok := colors[tag.Name]; ok && color != tag.Color {
			if err := tx.Model(tag).Update("color", color).Error; err != nil {
				return err
			}
		}
		delete(pending, tag.Name)
	}
	for name := range pending {
		if err := tx.Create(&model.Tag{Name: name, Color: colors[name], UserId: userId}).Error; err != nil {
			return err
		}
	}
	return nil
}

// InfoById 根据用户id取用户信息
func (s *TagService) InfoById(id uint) *model.Tag {
	u := &model.Tag{}
	DB.Where("id = ?", id).First(u)
	return u
}

func (s *TagService) List(page, pageSize uint, where func(tx *gorm.DB)) (res *model.TagList) {
	res = &model.TagList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.Tag{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.Tags)
	return
}

// Create 创建
func (s *TagService) Create(u *model.Tag) error {
	u.Id = 0
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, u.UserId); err != nil {
			return err
		}
		if err := checkUniqueTagName(tx, u); err != nil {
			return err
		}
		return tx.Create(u).Error
	})
}

func (s *TagService) Delete(u *model.Tag) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, u.UserId); err != nil {
			return err
		}
		if err := checkCurrentTag(tx, u); err != nil {
			return err
		}
		if err := rewriteTagReferences(tx, u, nil, false); err != nil {
			return err
		}
		return tx.Delete(u).Error
	})
}

// Update 更新
func (s *TagService) Update(current, patch *model.Tag) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, current.UserId, patch.UserId); err != nil {
			return err
		}
		if err := checkCurrentTag(tx, current); err != nil {
			return err
		}
		moving := current.UserId != patch.UserId
		if moving || current.Name != patch.Name {
			if err := checkUniqueTagName(tx, patch); err != nil {
				return err
			}
			if err := rewriteTagReferences(tx, current, &patch.Name, moving); err != nil {
				return err
			}
		}
		return tx.Model(current).Select("name", "user_id", "color").Updates(patch).Error
	})
}

func checkUniqueTagName(tx *gorm.DB, tag *model.Tag) error {
	var names []string
	if err := tagsForAccount(tx, tag.UserId).Where("name = ? AND id <> ?", tag.Name, tag.Id).Pluck("name", &names).Error; err != nil {
		return err
	}
	if slices.Contains(names, tag.Name) {
		return gorm.ErrDuplicatedKey
	}
	return nil
}

func checkCurrentTag(tx *gorm.DB, tag *model.Tag) error {
	var current model.Tag
	if err := tagsForAccount(tx, tag.UserId).Select("name").Where("id = ?", tag.Id).Take(&current).Error; err != nil {
		return err
	}
	if current.Name != tag.Name {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func rewriteTagReferences(tx *gorm.DB, tag *model.Tag, replacement *string, moving bool) error {
	var entries []model.AddressBook
	if err := tx.Select("row_id", "tags").Where("user_id = ?", tag.UserId).Find(&entries).Error; err != nil {
		return err
	}
	for _, entry := range entries {
		names, err := decodeAddressBookTags(entry.Tags)
		if err != nil {
			return err
		}
		if !slices.Contains(names, tag.Name) {
			continue
		}
		if moving {
			return ErrReferencedTagMove
		}
		if replacement == nil {
			names = slices.DeleteFunc(names, func(name string) bool { return name == tag.Name })
		} else {
			for i, name := range names {
				if name == tag.Name {
					names[i] = *replacement
				}
			}
		}
		encoded, err := json.Marshal(names)
		if err != nil {
			return err
		}
		if err := tx.Model(&entry).Update("tags", encoded).Error; err != nil {
			return err
		}
	}
	var peers []model.Peer
	if err := tx.Select("row_id", "tags").Where("user_id = ?", tag.UserId).Find(&peers).Error; err != nil {
		return err
	}
	for _, peer := range peers {
		names, err := decodeAddressBookTags(peer.Tags)
		if err != nil {
			return err
		}
		if !slices.Contains(names, tag.Name) {
			continue
		}
		if moving {
			return ErrReferencedTagMove
		}
		if replacement == nil {
			names = slices.DeleteFunc(names, func(name string) bool { return name == tag.Name })
		} else {
			for i, name := range names {
				if name == tag.Name {
					names[i] = *replacement
				}
			}
		}
		encoded, err := json.Marshal(names)
		if err != nil {
			return err
		}
		if err := tx.Model(&peer).Update("tags", encoded).Error; err != nil {
			return err
		}
	}
	return nil
}

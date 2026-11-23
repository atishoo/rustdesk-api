import { defineConfig } from 'vite';
import { resolve } from 'path';

export default defineConfig({
    build: {
        manifest: false,
        rollupOptions: {
            preserveEntrySignatures: 'strict',
            input: {
                index: resolve(__dirname, 'index.html'),
                remote: resolve(__dirname, 'src/remote.ts'),
            },
            output: {
                entryFileNames: `[name].js`,
                chunkFileNames: (chunk) => chunk.name === 'vendor' ? 'vendor.js' : '[name]-[hash].js',
                assetFileNames: `[name].[ext]`,
            }
        }
    },
})

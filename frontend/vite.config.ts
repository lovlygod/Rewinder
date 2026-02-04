import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import { copyFileSync } from 'fs'
import { resolve } from 'path'

export default defineConfig({
  plugins: [
    svelte(),
    {
      name: 'copy-icons',
      buildStart() {
        try {
          copyFileSync(
            resolve(__dirname, '../internal/public/appicon.png'),
            resolve(__dirname, 'public/appicon.png')
          )
        } catch (e) {
          console.warn('Could not copy appicon.png:', e)
        }
      }
    }
  ],
  publicDir: 'public'
})

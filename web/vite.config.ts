import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

/* Built output lands where Go embeds it.
 *
 * `internal/studio/webdist` rather than beside the old assets, so the two
 * front ends stay separable while the new one is being built: the existing
 * studio keeps working and serving at /, and this one is reachable at /v2
 * until it is at parity. Swapping them at the end is a one line change in the
 * server rather than a migration.
 */
export default defineConfig({
  plugins: [react()],
  base: '/v2/',
  build: {
    outDir: '../internal/studio/webdist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    /* `npm run dev` talks to a studio started separately, so the front end can
     * hot reload against real scores instead of fixtures. */
    proxy: {
      '/api': 'http://127.0.0.1:8799',
      '/media': 'http://127.0.0.1:8799',
    },
  },
  test: {
    globals: true,
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});

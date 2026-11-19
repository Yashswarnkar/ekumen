import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Go server (`ekumen serve`) listens on 127.0.0.1:7477 by default.
// Override with EKUMEN_API=http://host:port.
const api = process.env.EKUMEN_API ?? "http://127.0.0.1:7477";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: { "/api": api },
  },
  build: { outDir: "dist", emptyOutDir: true },
});

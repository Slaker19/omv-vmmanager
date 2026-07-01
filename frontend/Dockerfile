# Build context: repo root (matches the backend Dockerfile and
# the .github/workflows/ci.yml setup, which both use
# `context: .` so the same checkout works for both images).
FROM node:22-alpine AS build

WORKDIR /app
# Copy only the frontend's manifest first so the npm install
# layer is cached independently of the rest of the source tree.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

# Now bring in the Svelte sources. The trailing `./` after `COPY
# frontend/ .` matters: without it, the contents would be nested
# under `frontend/` and `npm run build` would look for
# `frontend/package.json` (not found).
COPY frontend/ ./
RUN npm run build

FROM nginx:stable-alpine

COPY --from=build /app/dist /usr/share/nginx/html
# nginx.conf is the only non-source file the image needs; it's
# kept in frontend/ alongside the rest of the frontend assets.
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]

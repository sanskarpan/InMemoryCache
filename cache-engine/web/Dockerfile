FROM node:24-alpine@sha256:2bdb65ed1dab192432bc31c95f94155ca5ad7fc1392fb7eb7526ab682fa5bf14 AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

RUN npm run build

FROM nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de

ENV CACHE_ENGINE_UPSTREAM_HOST=cache-engine-api

COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY 15-cache-engine-resolver.envsh /docker-entrypoint.d/15-cache-engine-resolver.envsh
RUN chmod +x /docker-entrypoint.d/15-cache-engine-resolver.envsh
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

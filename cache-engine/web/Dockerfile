FROM node:24-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

RUN npm run build

FROM nginx:1.29-alpine

ENV CACHE_ENGINE_UPSTREAM_HOST=cache-engine-api

COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY 15-cache-engine-resolver.envsh /docker-entrypoint.d/15-cache-engine-resolver.envsh
RUN chmod +x /docker-entrypoint.d/15-cache-engine-resolver.envsh
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

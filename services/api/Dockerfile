FROM node:22-slim AS base
WORKDIR /app

FROM base AS deps
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS dev
COPY . .
EXPOSE 3000
# npm install (not ci) on start: the bind mount's node_modules lives in a
# volume that may predate a dependency change; this keeps it honest.
CMD ["sh", "-c", "npm install --no-audit --no-fund && npm run start:dev"]

FROM deps AS build
COPY . .
RUN npm run build

FROM base AS prod
ENV NODE_ENV=production
COPY package.json package-lock.json ./
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY drizzle ./drizzle
RUN npm prune --omit=dev && npm cache clean --force
EXPOSE 3000
CMD ["node", "dist/main.js"]

# Zero runtime dependencies: Node's built-in http + crypto + Intl (full ICU with
# tzdata) cover everything this service needs, so nothing must be vendored and
# no package registry access is required even during build.
FROM node:20-alpine
WORKDIR /app
COPY package.json ./
COPY src ./src
ENV PORT=8080
EXPOSE 8080
USER node
CMD ["node", "src/server.js"]

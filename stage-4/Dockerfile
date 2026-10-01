# Zero runtime dependencies: Node's built-in http + crypto + Intl (full ICU with
# tzdata) cover the API, and the UI is hand-written vanilla HTML/CSS/JS served
# straight from this image. Nothing is fetched at build or run time beyond the
# base image itself.
FROM node:20-alpine
WORKDIR /app
COPY package.json ./
COPY src ./src
ENV PORT=8080
EXPOSE 8080
USER node
CMD ["node", "src/server.js"]

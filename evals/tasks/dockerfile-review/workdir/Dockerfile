FROM node:latest
WORKDIR /app
ADD . /app
RUN apt-get update && apt-get install -y curl
RUN npm install
ENV API_TOKEN=tk_9f8e7d6c5b4a3210ffee
EXPOSE 3000
CMD ["node", "server.js"]

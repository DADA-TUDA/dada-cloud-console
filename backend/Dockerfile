FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY bin/server ./server
COPY migrations/ ./migrations/
EXPOSE 8080
ENTRYPOINT ["./server"]

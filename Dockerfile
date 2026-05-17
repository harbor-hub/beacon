FROM alpine:latest
ARG TARGETARCH
RUN apk --no-cache add ca-certificates
WORKDIR /app/
ADD .build/beacon_linux_${TARGETARCH}.tar.gz .
EXPOSE 53/udp
ENTRYPOINT ["./beacon"]

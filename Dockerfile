# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN TARGETOS=$TARGETOS TARGETARCH=$TARGETARCH go run ./cmd/build -out /out
RUN mkdir -p /rootfs/app/auths /rootfs/app/data /rootfs/tmp \
 && chown -R 10001:10001 /rootfs/app/auths /rootfs/app/data \
 && chmod 1777 /rootfs/tmp

FROM scratch
ENV TZ=Asia/Shanghai
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /rootfs/ /
COPY --from=build /out/ /app/
COPY config.example.json /app/config.json
WORKDIR /app
USER 10001:10001
EXPOSE 7863
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
  CMD ["/app/wbapi", "healthcheck"]
ENTRYPOINT ["/app/wb2api", "-config", "/app/config.json"]

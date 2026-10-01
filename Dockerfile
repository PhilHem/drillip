FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates
RUN mkdir -m 1777 /runtime-tmp
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /drillip .

FROM scratch
COPY --from=build /runtime-tmp /tmp
COPY --from=build /drillip /drillip
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
VOLUME /data
ENV DRILLIP_DB=/data/errors.db
ENV DRILLIP_ADDR=0.0.0.0:8300
EXPOSE 8300
ENTRYPOINT ["/drillip"]

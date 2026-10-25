# Go standard library only: no module downloads are required.
FROM golang:1.26.9-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go test ./... && CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
ENV PORT=8080 APP_ENV=production
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]

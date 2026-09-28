FROM golang:1.26.6-alpine AS build

RUN apk add --no-cache git build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -o /out/git-drs .

FROM alpine:3.21
RUN apk add --no-cache bash ca-certificates curl git git-lfs jq openssl
COPY --from=build /out/git-drs /usr/local/bin/git-drs
COPY --from=build /src/tests/e2e-gen3-remote-full.sh /usr/local/share/git-drs/tests/e2e-gen3-remote-full.sh
ENTRYPOINT ["/bin/bash"]

# --- build the hshow binary ---
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal ./internal
COPY templates ./templates
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/hshow .

# --- runtime image, includes the hledger CLI ---
FROM debian:bookworm-slim
ARG HLEDGER_VERSION=1.52.4
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && curl -fL "https://github.com/hledgerorg/hledger/releases/download/${HLEDGER_VERSION}/hledger-linux-x64.tar.gz" \
       | tar -xzv -C /usr/local/bin hledger \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home --shell /usr/sbin/nologin hshow
COPY --from=build /out/hshow /usr/local/bin/hshow

USER hshow
ENV HSHOW_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/hshow"]

FROM docker.io/library/rust:1-slim-trixie AS build
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM docker.io/library/debian:trixie-slim
COPY --from=build /src/target/release/forge-proxy /usr/local/bin/forge-proxy
ENTRYPOINT ["forge-proxy"]

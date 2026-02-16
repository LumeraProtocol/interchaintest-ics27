# Dockerfile for Lumerad - Downloads pre-built binary from GitHub releases
# Build context should be the interchaintest directory (for claims.csv)

ARG LUMERA_VERSION=v1.10.1

FROM alpine:3.19

ARG LUMERA_VERSION

# Install runtime dependencies (libc6-compat needed for glibc-linked lumerad on Alpine)
RUN apk add --no-cache \
    ca-certificates \
    bash \
    jq \
    curl \
    libc6-compat \
    libgcc

# Download and install lumerad binary + libwasmvm from GitHub releases
# The real binary is installed as lumerad-bin; lumerad is a wrapper that strips
# --x-crisis-skip-assert-invariants (hardcoded by interchaintest but removed in v1.10.1)
RUN mkdir -p /tmp/release && \
    curl -sSfL "https://github.com/LumeraProtocol/lumera/releases/download/${LUMERA_VERSION}/lumera_${LUMERA_VERSION}_linux_amd64.tar.gz" \
    | tar -xz -C /tmp/release && \
    cp /tmp/release/lumerad /usr/local/bin/lumerad-bin && \
    chmod +x /usr/local/bin/lumerad-bin && \
    cp /tmp/release/libwasmvm.*.so /usr/local/lib/ 2>/dev/null || true && \
    rm -rf /tmp/release && \
    lumerad-bin version

# Create wrapper that strips --x-crisis-skip-assert-invariants (interchaintest
# hardcodes it, but the crisis module was removed in v1.10.1) and ensures
# claims.csv exists for the --claims-path flag.
RUN cat > /usr/local/bin/lumerad <<'WRAPPER'
#!/bin/bash
[ -f /tmp/claims.csv ] || touch /tmp/claims.csv
args=()
for arg in "$@"; do
  case "$arg" in
    --x-crisis-skip-assert-invariants) ;;
    *) args+=("$arg") ;;
  esac
done
exec lumerad-bin "${args[@]}"
WRAPPER
RUN chmod +x /usr/local/bin/lumerad

# Copy claims.csv (used via --claims-path /tmp/claims.csv in AdditionalStartArgs)
COPY claims.csv /tmp/claims.csv

# Create lumera user
RUN addgroup -g 1025 lumera && \
    adduser -D -u 1025 -G lumera lumera

USER lumera
WORKDIR /home/lumera

EXPOSE 26656 26657 1317 9090

CMD ["lumerad", "start"]

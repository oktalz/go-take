# gotake release image: scratch-based carrier for the static gotake binary.
# Assembled by GoReleaser from the per-platform build context; never compiled here.
FROM scratch

ARG TARGETPLATFORM
COPY --chmod=0755 ${TARGETPLATFORM}/gotake /usr/local/bin/gotake

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/gotake"]

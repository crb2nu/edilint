# Used by goreleaser (dockers_v2), whose build context holds one prebuilt
# static binary per target platform under $TARGETPLATFORM. There is
# deliberately nothing else in the image: edilint makes no network calls, so
# it needs no CA bundle, no shell and no libc, and an empty base keeps that
# verifiable.
#
#   docker run --rm -v "$PWD:/work" ghcr.io/crb2nu/edilint:<version> /work/claims.x12
FROM scratch
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/edilint /edilint
ENTRYPOINT ["/edilint"]

# gRPC certificate mount

This directory is a mount point, not a certificate store. Do not commit CA
keys, service private keys, or issued certificates.

Before starting the Compose example, provision these files through the
deployment secret manager:

```text
ca.crt
shop-user.crt
shop-user.key
shop-supplier.crt
shop-supplier.key
shop-order.crt
shop-order.key
```

Service certificates must be signed by `ca.crt`, permit both server and client
authentication, and include the service DNS name plus the configured gRPC
server name. Keep private keys mode `0600` and certificates mode `0644`. Compose
mounts the directory read-only at `/run/secrets/shop-grpc`.

Production deployments should inject short-lived identities from their secret
manager or use service-mesh mode. The repository intentionally contains no
usable certificate or private key.

#!/bin/bash

# Copyright 2026 KubeNexus Authors.
# 
# Script to generate self-signed TLS certificates for webhook
# For production, use cert-manager instead

set -e

WEBHOOK_NS="${WEBHOOK_NS:-kube-system}"
WEBHOOK_SVC="${WEBHOOK_SVC:-kubenexus-webhook}"
SECRET_NAME="${SECRET_NAME:-kubenexus-webhook-certs}"

echo "Generating TLS certificates for webhook..."
echo "Namespace: $WEBHOOK_NS"
echo "Service: $WEBHOOK_SVC"

# Create temp directory
TMPDIR=$(mktemp -d)
cd "$TMPDIR"

echo "Working in: $TMPDIR"

# Generate CA private key
openssl genrsa -out ca.key 2048

# Generate CA certificate
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 -out ca.crt \
  -subj "/CN=KubeNexus Webhook CA"

# Generate webhook private key
openssl genrsa -out tls.key 2048

# Generate CSR
cat > csr.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name

[req_distinguished_name]

[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${WEBHOOK_SVC}
DNS.2 = ${WEBHOOK_SVC}.${WEBHOOK_NS}
DNS.3 = ${WEBHOOK_SVC}.${WEBHOOK_NS}.svc
DNS.4 = ${WEBHOOK_SVC}.${WEBHOOK_NS}.svc.cluster.local
EOF

openssl req -new -key tls.key -out tls.csr -subj "/CN=${WEBHOOK_SVC}.${WEBHOOK_NS}.svc" -config csr.conf

# Sign certificate
openssl x509 -req -in tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out tls.crt -days 365 -extensions v3_req -extfile csr.conf

echo ""
echo "Certificates generated successfully!"
echo ""

# Create Kubernetes secret
echo "Creating secret in namespace $WEBHOOK_NS..."
kubectl create secret generic "$SECRET_NAME" \
  --from-file=tls.crt=tls.crt \
  --from-file=tls.key=tls.key \
  --namespace="$WEBHOOK_NS" \
  --dry-run=client -o yaml | kubectl apply -f -

echo ""
echo "Secret created: $SECRET_NAME"
echo ""

# Get CA bundle for webhook configuration
CA_BUNDLE=$(cat ca.crt | base64 | tr -d '\n')

echo "CA Bundle for webhook configuration:"
echo "$CA_BUNDLE"
echo ""

# Update webhook configuration with CA bundle
if [ -f ../deploy/webhook.yaml ]; then
  echo "Updating deploy/webhook.yaml with CA bundle..."
  sed "s/\${CA_BUNDLE}/$CA_BUNDLE/" ../deploy/webhook.yaml > ../deploy/webhook-configured.yaml
  echo "Updated configuration saved to: deploy/webhook-configured.yaml"
  echo ""
  echo "Apply with: kubectl apply -f deploy/webhook-configured.yaml"
else
  echo "To update webhook configuration, replace \${CA_BUNDLE} with:"
  echo "$CA_BUNDLE"
fi

# Cleanup
cd -
rm -rf "$TMPDIR"

echo ""
echo "✅ Certificate generation complete!"
echo ""
echo "Next steps:"
echo "1. Build webhook image: make docker-build-webhook"
echo "2. Push image: make docker-push-webhook"
echo "3. Deploy webhook: kubectl apply -f deploy/webhook-configured.yaml"

#!/bin/bash

set -e

# setup directories
mkdir -p testdata/certs
cd testdata/certs

echo "<!-----------------------------------!>"
echo "<! Certificate Authority Certificate !>"
echo "<!-----------------------------------!>"

echo "Generating certificate authority private key..."
openssl genrsa -out root-ca.key 4096

echo "Generating certificate authority configuration file..."
cat <<EOF > root-ca.cnf
[ req ]
default_bits       = 4096
default_keyfile    = root-ca.key
distinguished_name = req_distinguished_name
req_extensions     = v3_ca
prompt             = no

[ req_distinguished_name ]
C  = US
ST = VIRGINIA
L  = RESTON
O  = HAULER
OU = HAULER DEV
CN = CERTIFICATE AUTHORITY CERTIFICATE

[v3_ca]
keyUsage = critical, keyCertSign, cRLSign
extendedKeyUsage = anyExtendedKeyUsage
basicConstraints = critical, CA:TRUE
EOF

echo "Generating certificate authority certificate signing request..."
openssl req -new -sha256 -key root-ca.key -out root-ca.csr -config root-ca.cnf

echo "Generating certificate authority certificate..."
openssl x509 -req -in root-ca.csr -signkey root-ca.key -days 3650 -out root-ca.crt -extensions v3_ca -extfile root-ca.cnf

echo "Inspecting certificate authority certificate..."
openssl x509 -text -noout -in root-ca.crt > ca.txt

echo "<!------------------------------------------------!>"
echo "<! Intermediary Certificate Authority Certificate !>"
echo "<!------------------------------------------------!>"

echo "Generating intermediary certificate authority private key..."
openssl genrsa -out intermediary-ca.key 4096

echo "Generating intermediary certificate authority configuration file..."
cat <<EOF > intermediary-ca.cnf
[ req ]
default_bits       = 4096
default_keyfile    = intermediary-ca.key
distinguished_name = req_distinguished_name
req_extensions     = v3_ca
prompt             = no

[ req_distinguished_name ]
C  = US
ST = VIRGINIA
L  = RESTON
O  = HAULER
OU = HAULER DEV
CN = INTERMEDIARY CERTIFICATE AUTHORITY CERTIFICATE

[v3_ca]
keyUsage = critical, keyCertSign, cRLSign
extendedKeyUsage = anyExtendedKeyUsage
basicConstraints = critical, CA:TRUE
EOF

echo "Generating intermediary certificate authority certificate signing request..."
openssl req -new -sha256 -key intermediary-ca.key -out intermediary-ca.csr -config intermediary-ca.cnf

echo "Generating intermediary certificate authority certificate..."
openssl x509 -req -in intermediary-ca.csr -CA root-ca.crt -CAkey root-ca.key -CAcreateserial -out intermediary-ca.crt -days 3650 -sha256 -extfile intermediary-ca.cnf -extensions v3_ca

echo "Inspecting intermediary certificate authority certificate..."
openssl x509 -text -noout -in intermediary-ca.crt > intermediary-ca.txt

echo "Verifying intermediary certificate authority certificate..."
openssl verify -CAfile root-ca.crt intermediary-ca.crt

echo "Generating full certificate chain..."
cat intermediary-ca.crt root-ca.crt > cacerts.pem

echo "<!-----------------------------------------------------------------!>"
echo "<! Server Certificate Signed by Intermediary Certificate Authority !>"
echo "<!-----------------------------------------------------------------!>"

echo "Generating server private key..."
openssl genrsa -out server-cert.key 4096

echo "Generating server certificate signing config file..."
cat <<EOF > server-cert.cnf
[ req ]
default_bits       = 4096
default_keyfile    = server-cert.key
distinguished_name = req_distinguished_name
req_extensions     = v3_req
prompt             = no

[ req_distinguished_name ]
C  = US
ST = VIRGINIA
L  = RESTON
O  = HAULER
OU = HAULER DEV
CN = SERVER CERTIFICATE

[v3_req]
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = localhost
DNS.2 = registry.localhost
DNS.3 = fileserver.localhost
EOF

echo "Generating server certificate signing request..."
openssl req -new -sha256 -key server-cert.key -out server-cert.csr -config server-cert.cnf

echo "Generating server certificate..."
openssl x509 -req -in server-cert.csr -CA intermediary-ca.crt -CAkey intermediary-ca.key -CAcreateserial -out server-cert.crt -days 3650 -sha256 -extfile server-cert.cnf -extensions v3_req

echo "Inspecting server certificate..."
openssl x509 -text -noout -in server-cert.crt > server-cert.txt

echo "Verifying server certificate..."
openssl verify -CAfile cacerts.pem server-cert.crt

echo "<!-----------------------------------------------------------------!>"
echo "<! Client Certificate Signed by Intermediary Certificate Authority !>"
echo "<!-----------------------------------------------------------------!>"

echo "Generating client private key..."
openssl genrsa -out client-cert.key 4096

echo "Generating client certificate signing config file..."
cat <<EOF > client-cert.cnf
[ req ]
default_bits       = 4096
default_keyfile    = client-cert.key
distinguished_name = req_distinguished_name
req_extensions     = v3_req
prompt             = no

[ req_distinguished_name ]
C  = US
ST = VIRGINIA
L  = RESTON
O  = HAULER
OU = HAULER DEV
CN = CLIENT CERTIFICATE

[ v3_req ]
keyUsage = digitalSignature
extendedKeyUsage = clientAuth
EOF

echo "Generating client certificate signing request..."
openssl req -new -sha256 -key client-cert.key -out client-cert.csr -config client-cert.cnf

echo "Generating client certificate..."
openssl x509 -req -in client-cert.csr -CA intermediary-ca.crt -CAkey intermediary-ca.key -CAcreateserial -out client-cert.crt -days 3650 -sha256 -extfile client-cert.cnf -extensions v3_req

echo "Inspecting client certificate..."
openssl x509 -text -noout -in client-cert.crt > client-cert.txt

echo "Verifying client certificate..."
openssl verify -CAfile cacerts.pem client-cert.crt

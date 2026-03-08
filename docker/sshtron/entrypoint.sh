#!/bin/sh
set -e
cd /tmp
ssh-keygen -t rsa -f id_rsa -N "" -q
exec /usr/local/bin/sshtron

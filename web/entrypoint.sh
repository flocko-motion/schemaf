#!/bin/sh
envsubst '${BACKEND_URL} ${FRONTEND_URL}' \
  < /etc/nginx/templates/nginx.conf.template \
  > /etc/nginx/conf.d/default.conf
exec nginx -g 'daemon off;'

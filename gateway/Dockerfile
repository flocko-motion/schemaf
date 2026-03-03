FROM nginx:alpine
COPY nginx.conf.template /etc/nginx/templates/
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]

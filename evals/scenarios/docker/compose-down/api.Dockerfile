FROM busybox:1.37
COPY api-entrypoint.sh /api-entrypoint.sh
RUN chmod +x /api-entrypoint.sh
ENTRYPOINT ["/api-entrypoint.sh"]

FROM busybox
COPY bin/kube-scheduler /opt/
RUN mkdir -p /etc/config/ 
COPY config/config.yaml /etc/config/config.yaml
#ENTRYPOINT ["/opt/kube-scheduler"]


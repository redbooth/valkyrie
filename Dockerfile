FROM scratch
ADD bin/valkyrie /valkyrie
EXPOSE 8888
ENTRYPOINT ["/valkyrie"]

# docker-flatten

this is a proof of concept on how we can flatten supplied docker image's data
layers into one single layer while trying to retain as much metadata as possible

## Run
```
sudo go run main.go -t newtag existing_image_id
```

If success, it will generate a new image with `newtag` tag. For example, if we
have an old image with histories:
```
ID                       CREATED             CREATED BY
docker/flatten:latest    42 minutes ago      /bin/sh -c #(nop) CMD [/bin/sh -c "/bin/echo" "Hello World"]
8e2e9518c2e0             42 minutes ago      /bin/sh -c #(nop) EXPOSE [123 234]
77b2f0a45347             19 hours ago        /bin/sh -c echo "finished"
147b4ee1963f             19 hours ago        /bin/sh -c echo Layer 1 >> layerfile1.txt
a621f6922e55             19 hours ago        /bin/sh -c rm layerfile.txt
192a8f21b35a             19 hours ago        /bin/sh -c apt-get update
9a79eb7d79bb             19 hours ago        /bin/sh -c echo Layer 2 >> layerfile.txt
933fe71d3d56             20 hours ago        /bin/sh -c echo Layer 0 >> layerfile.txt
34c25748f8e8             20 hours ago        /bin/sh -c #(nop) MAINTAINER dqminh "dqminh89@gmail.com"
ubuntu:12.04             4 months ago
```

The new generated image's histories will be:
```
ID                          CREATED             CREATED BY
docker/newflatten:latest    13 minutes ago      /bin/sh -c #(nop) CMD [/bin/sh -c "/bin/echo" "Hello World"]
ceed8ef154f6                13 minutes ago      /bin/sh -c #(nop) EXPOSE [123 234]
0e4a4c56d203                14 minutes ago      /bin/sh -c #(nop) ADD docker-flatten773823896.tar.gz in /
34c25748f8e8                20 hours ago        /bin/sh -c #(nop) MAINTAINER dqminh "dqminh89@gmail.com"
ubuntu:12.04                4 months ago
```

## How is this being done
- gather histories of the existing image
- from the history, gather info for each image layer, except the base layer
- rsync all data from base layers into a new directory
- remove whiteouts and supposedly deleted files/directories
- compress the data into a tarfile
- prepare a directory with the tarfile and a generated dockerfile with as much
  metadata as possible
- create a new image from the generated dockerfile

# Getting Started with hauler using Docker Compose

This guide will walk you through the process of setting up and running the hauler using Docker Compose.

The [Dockerfile](./docker/Dockerfile) is basesd on the alpine:3.19 image and installs the hauler binary.
Upon startup it looks for any haul tarball files and load them , it also loads the hauls using the Hauler Manifests if present.

## Prerequisites

Before you begin, ensure you have the following installed on your system:

- Docker: Follow the [official Docker installation guide](https://docs.docker.com/get-docker/) to install Docker on your system.
- Docker Compose: Follow the [official Docker Compose installation guide](https://docs.docker.com/compose/install/) to install Docker Compose.


## Starting the Application

To start the hauler, follow these steps:

1. Open a terminal and navigate to the project directory.

2. Run the following command to start the hauler using Docker Compose:

```bash
cd docker
docker compose build
docker compose build up -d
```

The `-d` flag runs the containers in detached mode, meaning they will run in the background.


4. Once the containers are up and running, you can access the hauler registry using your web browser at http://localhost:5000.

# Entering the hauler container

To exec into the hauler contaienr and run commands, run the following command:

```bash
❯ nerdctl exec -it hauler /bin/sh
/home/hauler # hauler store list
+----------------------+-------+---------------+----------+---------+
| REFERENCE            | TYPE  | PLATFORM      | # LAYERS | SIZE    |
+----------------------+-------+---------------+----------+---------+
| hauler/rancher:2.8.2 | chart | -             |        1 | 15.0 kB |
| library/alpine:3.19  | image | linux/386     |        1 | 3.2 MB  |
|                      | image | linux/amd64   |        1 | 3.4 MB  |
|                      | image | linux/arm     |        1 | 3.2 MB  |
|                      | image | linux/arm     |        1 | 2.9 MB  |
|                      | image | linux/arm64   |        1 | 3.3 MB  |
|                      | image | linux/ppc64le |        1 | 3.4 MB  |
|                      | image | linux/s390x   |        1 | 3.2 MB  |
+----------------------+-------+---------------+----------+---------+
|                                                 TOTAL   | 22.7 MB |
+----------------------+-------+---------------+----------+---------+
/home/hauler #
```


## Stopping the hauler

To stop the 5000 and remove the Docker containers, run the following command in the project directory:

```bash
dockercompose down
```

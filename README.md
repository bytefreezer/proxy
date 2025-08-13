# bytefreezer-proxy Service


This service is intended to be used as a proxy for the bytefreezer ebpf agent to S3 or to Webhook destination.
This is standard service that can be used to proxy the data from multiple bytefreezer agents to the destination.

bytefreezerA--udp:2056--\
                          ---> bytefreezer-proxy-service ---> [S3 | webhook]
bytefreezerB--udp:2056--/                              

This distribution consists of the following components:
1. example configuration file
2. Docker-compose file


to install the service:
1. untar the distributios as follows:
```tar -xvf bytefreezer-proxy.tar.gz
```
2. cd bytefreezer-proxy
3. edit the configuration file to point to and configure destination

4. configure udp buffer limits on a docker host machine, to match the config.yaml (default read_buffer_size_kb:8000)
```sysctl -w net.core.rmem_max=8000
   sysctl -w net.core.rmem_default=8000
   sysctl -w net.core.wmem_max=8000
   sysctl -w net.core.wmem_default=8000
```
5. create cache directory
```mkdir -p /tmp/bytefreezer-proxy
```
6. edit ./etc/config.yaml file to match port mapping of that of the bytefreezer agent
```ports:
      - "2056:2056/udp"
```

7. edit s3 /webhook configuration in the config.yaml file

8. run the docker-compose file
```docker-compose up -d
```

The service will start and start listening on udp port 2056, and will start proxying the data to the destination.

monitor with the following command:
```docker logs -f <container id>
```
based on your configuration settings you should see uploads
when file reaches defined batch size or every 30 seconds.

```
   #batch limits number of rows
  s3_batch_size: 1000000
  #batch limits time in seconds before flash to s3
  s3_batch_timeout_sec: 30
  ```
this is service runs udp listener to receive data line by line from external sources. example syslog or ebpf client
then pack it into n lines or n bytes per configuration and post to bytefreezer-receiver compressed, please construct uri accordig to config, consult with ../bytefreezer-receiver uri format
the api at the moment only has health endpoint and display config
this service is to be installed on premises of heavy users with udp use.

the configuration for udp is tenant token and data set id to match bytefreezer-receiver uri format
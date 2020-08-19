#!/bin/bash
# spawn scp ./consensus/consensusMy/service/consensus.go root@$ip:/go/src/BCDns_0.1/consensus/consensusMy/service/consensus.go

cat /tmp/hosts | while read line
do
    ip=$(echo $line | awk '{print $1}')
    hostname=$(echo $line | awk '{print $2}')
    if [[ $HOST == $hostname ]]; then
        continue
    fi

    expect -c "
    spawn scp ./network/service/networkDirect.go root@$ip:/go/src/BCDns_0.1/network/service/networkDirect.go
    expect {
        \"*yes/no*\" {send \"yes\r\";exp_continue;}
        \"*assword\" {set timeout 300; send \"NSL2020\r\"; exp_continue;}
    }"
done

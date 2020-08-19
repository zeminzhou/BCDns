#! /bin/bash
cat /tmp/hosts | while read line
do
    ip=$(echo $line | awk '{print $1}')
    hostname=$(echo $line | awk '{print $2}')
    expect -c "
    spawn ssh root@$ip \"(source /etc/profile && cd /go/src/BCDns_daemon && nohup go run main.go) > /dev/null 2>&1 &\"
    expect {
        \"*yes/no*\" {send \"yes\r\";exp_continue;}
        \"*assword\" {set timeout 300; send \"NSL2020\r\"; exp_continue;}
    }"
done

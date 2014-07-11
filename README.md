pingd
=====


Simple ping daemon with RESTful JSON API and graphite export


Example
-------


``` sh
pingd --api 127.0.0.1:8081 --graphite 127.0.0.1:2003

curl -i -d '{"Id":"router1","Address":"192.168.1.1"}' http://127.0.0.1:8081/hosts # add ip
curl -i http://127.0.0.1:8081/hosts/router1  # get stat
curl -i http://127.0.0.1:8081/hosts          # get all stat
curl -i -X DELETE http://127.0.0.1:8081/hosts/rouer1
```

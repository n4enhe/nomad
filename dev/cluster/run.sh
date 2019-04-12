#!/bin/sh
for c in 1 2 3; do
    nomad agent -dev -config=server$c.hcl > /tmp/cluster-server$c.log 2>&1 &
done

for c in 1 2 3; do
    nomad agent -config=client$c.hcl > /tmp/cluster-client$c.log 2>&1 &
done

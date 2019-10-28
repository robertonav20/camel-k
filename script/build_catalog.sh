#!/bin/sh

# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

location=$(dirname $0)
rootdir=$location/../

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    echo "usage: $0 catalog.version runtime.version [staging.repo]"
    exit 1
fi

$rootdir/mvnw -q \
    -f ${rootdir}/build/maven/pom-catalog.xml \
    -Dcatalog.path=${rootdir}/deploy \
    -Dcamel.version=$1 \
    -Druntime.version=$2 \
    -Dstaging.repo=$3

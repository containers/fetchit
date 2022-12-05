#!/bin/bash
set -e -o pipefail


main() {
	echo "initializing fetchit"
	if [[ -n "${FETCHIT_CONFIG}" ]]; then
		declare fetchitConfigFile='/opt/mount/config.yaml'
		echo "detected config environment variable"
		if ! [ -d '/opt/mount' ]; then
			echo "/opt/mount doesn't exist, creating"
			mkdir -p /opt/mount
		fi

		if [ -f "${fetchitConfigFile}" ]; then
			echo 'overriding config with variable'
		fi
		echo "${FETCHIT_CONFIG}" > "${fetchitConfigFile}"
		echo "wrote new config to ${fetchitConfigFile}"
	fi

	echo 'starting fetchit'
 	/usr/local/bin/fetchit start
}

main
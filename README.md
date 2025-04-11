# Assumptions And Usages
- The health check endpoints are assumed to be accessed by http request.
- The health check endpoints on all devices are assumed to have the same url path: `/health`, even though they can listen on different ports.
- Response from the health check endpoint contains the protocols the device supports for diagnostics data polling. For each protocol (grpc and rest), the response can optionally include the port and path of the data polling endpoint ( only for rest ) specific to the device. Otherwise, default ports and path for grpc and rest endpoints are used.
- Devices to be monitored can be added to the database dynamically by calling the `PUT /devices` endpoint of this service. In the request, the hostname and port of the HTTP health check endpoint are required.
- A separate worker process needs to be started to actually poll the data of the devices.
- The `pkg/device_simulator.go` contains code to simulate the hehaviour of a virtual device. It has chaning states, thus returning normal or error responses to the data polling requests, based on a internal transition period which is currently hard coded as 10 seconds, but can be extended to be more flexible easily.
- The `pkg` package also contains a function `ExecuteExternalChecksumGenerator` that can be used by the devices to call the external checksum generator executable binary, provided that the binary is present on the file system point by the env variable 'EXTERNAL_CHECKSUM_GENERATOR_LOCATION'.
- A proof of concept of all the parts working together can be done by executing `make poc` under the project root directory, it will start the database, the web service, the polling worker, and 3 device simulators running as containers on your local machine.
Then you can manually check the health endpoints of the 3 virtual devices to get their device ids, and device types, and use the information to add theses devices to the monitoring system by calling its rest endpoint `PUT /devices`.

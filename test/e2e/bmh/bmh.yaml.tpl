apiVersion: v1
kind: Secret
metadata:
  name: REPLACE_NAME-secret
type: Opaque
stringData:
  username: YWRtaW4=
  password: cGFzc3dvcmQ=
---
apiVersion: metal3.io/v1alpha1
kind: BareMetalHost
metadata:
  annotations:
    inspect.metal3.io: disabled
  name: REPLACE_NAME
spec:
  online: true
  bootMACAddress: REPLACE_MAC
  bootMode: UEFI
  bmc:
    address: redfish-virtualmedia+http://192.168.222.1:8000/redfish/v1/Systems/REPLACE_ID
    credentialsName: REPLACE_NAME-secret
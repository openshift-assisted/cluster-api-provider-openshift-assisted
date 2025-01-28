<network xmlns:dnsmasq='http://libvirt.org/schemas/network/dnsmasq/1.0' connections='5'>
  <name>bmh</name>
  <forward mode='nat'>
    <nat>
      <port start='1024' end='65535'/>
    </nat>
  </forward>
  <bridge name='bmh' stp='on' delay='0'/>
  <mac address='52:54:00:fb:8a:5f'/>
  <ip address='192.168.222.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.222.2' end='192.168.222.254'/>
      <host mac='00:60:2f:31:81:00' name='okd-0' ip='192.168.222.30'/>
      <host mac='00:60:2f:31:81:01' name='okd-1' ip='192.168.222.31'/>
      <host mac='00:60:2f:31:81:02' name='okd-2' ip='192.168.222.32'/>
      <host mac='00:60:2f:31:81:03' name='okd-3' ip='192.168.222.33'/>
      <host mac='00:60:2f:31:81:04' name='okd-4' ip='192.168.222.34'/>
      <host mac='00:60:2f:31:81:05' name='okd-5' ip='192.168.222.35'/>
      <host mac='00:60:2f:31:81:06' name='okd-6' ip='192.168.222.36'/>
      <host mac='00:60:2f:31:81:07' name='okd-7' ip='192.168.222.37'/>
      <host mac='00:60:2f:31:81:08' name='okd-8' ip='192.168.222.38'/>
      <host mac='00:60:2f:31:81:09' name='okd-9' ip='192.168.222.39'/>
    </dhcp>
  </ip>
  <dnsmasq:options>
    <dnsmasq:option value='local=/lab.home/'/>
    <dnsmasq:option value='address=/api.test-sno.lab.home/192.168.222.31'/>
    <dnsmasq:option value='address=/api-int.test-sno.lab.home/192.168.222.31'/>
    <dnsmasq:option value='address=/.apps.test-sno.lab.home/192.168.222.31'/>
    <dnsmasq:option value='address=/api.test-multinode.lab.home/192.168.222.40'/>
    <dnsmasq:option value='address=/api-int.test-multinode.lab.home/192.168.222.40'/>
    <dnsmasq:option value='address=/.apps.test-multinode.lab.home/192.168.222.41'/>
    <dnsmasq:option value='address=/assisted-service.assisted-installer.com/<LOADBALANCER_IP>'/>
    <dnsmasq:option value='address=/assisted-image.assisted-installer.com/<LOADBALANCER_IP>'/>
    <dnsmasq:option value='server=/registry-proxy.engineering.redhat.com/10.11.5.160'/>
    <dnsmasq:option value='server=8.8.8.8'/>
  </dnsmasq:options>
</network>
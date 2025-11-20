# GateLAN using BADVPN and OpenVPN
Routing traffic from a Windows TUN/TAP virtual interface through a proxy is a common requirement for bypassing network restrictions, enhancing privacy, or testing. This is not a built-in Windows feature, so it requires third-party software.

Here's a comprehensive guide covering the concepts, tools, and step-by-step instructions.

### Core Concepts

1.  *TUN/TAP Driver:* This is a virtual network adapter installed on your Windows system.
    *   *TAP (Layer 2):* Simulates an Ethernet link. It receives raw Ethernet frames.
    *   *TUN (Layer 3):* Simulates a network link. It receives IP packets. *For proxying, TUN is almost always the correct choice.*

2.  *Tun-to-Proxy Application:* This is the "bridge" software. It reads IP packets from the TUN interface, re-encapsulates them into the proxy protocol (like SOCKS5 or HTTP), and sends them to your proxy server.

3.  *Proxy Server:* The remote server that your traffic will be forwarded through. This can be a SOCKS5, HTTP, or a more advanced proxy.

4.  *Routing Configuration:* You must tell Windows to send the desired traffic to the virtual TUN interface instead of your default physical network card.

---

### Recommended Tools & Solutions

There are several ways to achieve this, ranging from classic command-line tools to modern, all-in-one GUI applications.

#### Method 1: The Classic Approach (Advanced Users)

This method gives you the most control but requires manual configuration. The most famous tool for this is badvpn-tun2socks.

*Tools Needed:*
1.  *TAP-Windows Driver:* The virtual network driver. The easiest way to get this is by installing [OpenVPN](https://openvpn.net/community-downloads/). During installation, make sure the "TAP-Windows6" component is selected.
2.  *badvpn-tun2socks:* A lightweight, standalone executable that does the packet forwarding. You can download pre-compiled binaries from its GitHub releases page or other community sources.

*Step-by-Step Guide:*

*Step 1: Install and Configure the TAP Adapter*
1.  Install OpenVPN to get the TAP driver.
2.  Go to Control Panel > Network and Internet > Network and Sharing Center > Change adapter settings.
3.  You should see an adapter named "TAP-Windows6 V9" or similar. Rename it to something simple, like mytap.
4.  Right-click mytap > Properties.
5.  Double-click Internet Protocol Version 4 (TCP/IPv4).
6.  Set a static IP address and netmask. This will be the gateway for your tunneled traffic.
    *   *IP address:* 10.0.0.1
    *   *Subnet mask:* 255.255.255.0
    *   Click OK.

*Step 2: Run tun2socks*
Open a Command Prompt or PowerShell as an administrator. Navigate to where you saved badvpn-tun2socks.exe.

The command structure is:

badvpn-tun2socks.exe --tundev "mytap" --netif-ipaddr 10.0.0.2 --netif-netmask 255.255.255.0 --socks-server-addr <PROXY_IP>:<PROXY_PORT>


*   --tundev "mytap": The name of your TAP adapter.
*   --netif-ipaddr 10.0.0.2: The IP address for the tun2socks application itself on the virtual network. It must be on the same subnet as the TAP adapter but different from its IP.
*   --netif-netmask 255.255.255.0: The subnet mask, matching the TAP adapter.
*   --socks-server-addr <PROXY_IP>:<PROXY_PORT>: The address of your SOCKS5 proxy server.

*Example:*
If your SOCKS5 proxy is at 192.168.1.100:1080, the command would be:

badvpn-tun2socks.exe --tundev "mytap" --netif-ipaddr 10.0.0.2 --netif-netmask 255.255.255.0 --socks-server-addr 192.168.1.100:1080

Leave this command prompt window running.

*Step 3: Configure Windows Routing*
This is the most critical step. You need to add routes that tell Windows to send traffic through the mytap interface.

1.  *Find your main gateway:* Open Command Prompt and run ipconfig. Look for your primary Ethernet or Wi-Fi adapter and note its "Default Gateway" (e.g., 192.168.1.1).
2.  *Find your proxy server's IP:* (e.g., 192.168.1.100).
3.  *Add the routes:*

    cmd
    :: IMPORTANT: Route traffic to the proxy server DIRECTLY, not through the tunnel.
    :: This prevents a routing loop.
    route add <PROXY_IP> MASK 255.255.255.255 <YOUR_MAIN_GATEWAY>

    :: Route all other traffic through the TUN interface.
    :: The gateway here is the IP of the tun2socks app.
    route add 0.0.0.0 MASK 0.0.0.0 10.0.0.2 METRIC 1
    

    *Example:*
    cmd
    route add 192.168.1.100 MASK 255.255.255.255 192.168.1.1
    route add 0.0.0.0 MASK 0.0.0.0 10.0.0.2 METRIC 1
    

Now, all your traffic (except traffic to the proxy itself) will be routed through tun2socks and to your proxy server.

*To remove the routes later:*
cmd
route delete <PROXY_IP>
route delete 0.0.0.0


---

#### Method 2: The Modern All-in-One Approach (Recommended for Most Users)

Modern proxy clients like *Clash* or *v2rayN* have built-in TUN mode, which automates the entire process above. They manage the TAP driver, the packet forwarding, and the routing rules for you.

*Tool Recommendation: Clash for Windows*

1.  *Download and Install:* Get the latest version of [Clash for Windows](https://github.com/Fndroid/clash_for_windows_pkg/releases) (or a fork if the original is discontinued).
2.  *Get a Configuration:* You need a config.yaml file that defines your proxy servers. This can be provided by your proxy service or created manually.
3.  *Enable TUN Mode:*
    *   Start Clash and load your configuration.
    *   Go to the "Settings" tab.
    *   Find the "TUN Mode" option and enable it.
    *   Clash will automatically install a TAP driver (if not present), configure it, and set up the necessary routes.

That's it! With a single click, Clash handles everything. It also correctly manages DNS requests to prevent leaks, which is a common issue with the manual method.

---

### Important Considerations

*   *DNS Leaks:* This is a major risk. If your DNS requests go through your normal network connection instead of the tunnel, your activity can be monitored.
    *   *Manual Fix:* After setting up the routes, manually change your network adapter's DNS settings to a public DNS like 8.8.8.8 (Google) or 1.1.1.1 (Cloudflare).
    *   *Modern Tool Fix:* Tools like Clash have a "DNS Hijacking" feature that automatically handles this, ensuring all DNS goes through the proxy.

*   *Routing Loops:* Always ensure you have a specific route for your proxy server that points to your real gateway, not the TUN interface. Otherwise, the proxy traffic will try to go through itself, creating a loop.

*   *Firewall/Antivirus:* Windows Defender Firewall or third-party antivirus software might block the TAP adapter or the tun2socks application. You may need to create rules to allow them.

*   *Performance:* There is always some performance overhead due to the packet processing and routing. It will be slightly slower than a direct connection.

### Summary

| Feature | Manual tun2socks | Modern GUI (Clash) |
| :--- | :--- | :--- |
| *Ease of Use* | Complex, requires command line | Easy, GUI-based |
| *Control* | Maximum, granular control | Good, but relies on app's logic |
| *DNS Handling* | Manual configuration required | Automatic (DNS Hijacking) |
| *Setup Time* | High | Low |
| *Best For* | Advanced users, custom scripts, learning | Beginners, daily use, reliability |

For most users today, using a modern client like *Clash for Windows* is the superior and far simpler solution. If you need to build a custom solution or understand the underlying mechanics, the badvpn-tun2socks method is the classic and powerful way to do it.

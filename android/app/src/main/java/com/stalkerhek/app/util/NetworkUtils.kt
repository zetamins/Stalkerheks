package com.stalkerhek.app.util

import java.net.Inet4Address
import java.net.NetworkInterface

fun getLocalIpAddress(): String {
    try {
        // Collect every usable IPv4, then pick the best by interface type. On a
        // phone with Wi-Fi AND cellular/VPN up at once, returning the first hit
        // could hand out a cellular/VPN address that other LAN devices (and the
        // QR/dashboard URL) can't reach — so rank Wi-Fi/Ethernet ahead of
        // cellular (rmnet/ccmni/clat), VPN/tunnels (tun/ppp), and emulator NAT.
        val candidates = mutableListOf<Pair<Int, String>>() // (rank, ip) — lower rank wins
        val interfaces = NetworkInterface.getNetworkInterfaces()
        while (interfaces.hasMoreElements()) {
            val netInterface = interfaces.nextElement()
            if (netInterface.isLoopback || !netInterface.isUp) continue
            val name = (netInterface.name ?: "").lowercase()
            val addresses = netInterface.inetAddresses
            while (addresses.hasMoreElements()) {
                val addr = addresses.nextElement()
                if (addr is Inet4Address && !addr.isLoopbackAddress) {
                    val ip = addr.hostAddress ?: continue
                    if (ip.startsWith("127.")) continue
                    val virtual = ip.startsWith("192.168.240.") ||
                        ip.startsWith("192.168.250.") || ip.startsWith("10.0.2.")
                    val rank = when {
                        virtual -> 5
                        name.startsWith("wlan") || name.startsWith("ap") -> 0
                        name.startsWith("eth") -> 1
                        name.startsWith("tun") || name.startsWith("ppp") -> 4
                        name.startsWith("rmnet") || name.startsWith("ccmni") ||
                            name.startsWith("clat") || name.startsWith("rndis") -> 3
                        else -> 2
                    }
                    candidates.add(rank to ip)
                }
            }
        }
        candidates.minByOrNull { it.first }?.let { return it.second }
    } catch (_: Exception) {}
    return "127.0.0.1"
}

#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
Shared URL safety check for tools that fetch something a user named.
"""

import ipaddress
import socket
import urllib.parse
import urllib.request

def safe_url(url: str):
    """Return (parsed, None) if the URL is safe to fetch, else (None, reason).

    Checks every resolved address, not the hostname, so a name pointing at a
    private range is refused too.
    """
    try:
        p = urllib.parse.urlparse(url)
    except ValueError:
        return None, "that is not a url"
    if p.scheme not in ("http", "https"):
        return None, "only http and https links"
    if not p.hostname:
        return None, "that url has no host"

    try:
        infos = socket.getaddrinfo(p.hostname, None)
    except socket.gaierror:
        return None, "could not resolve that host"

    for info in infos:
        addr = ipaddress.ip_address(info[4][0])
        if (addr.is_private or addr.is_loopback or addr.is_link_local
                or addr.is_reserved or addr.is_multicast or addr.is_unspecified):
            return None, "that address is not public"
    return p, None

class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Refuse redirects: a public URL could redirect to an address safe_url never checked."""

    def redirect_request(self, *args, **kwargs):
        return None

def opener():
    return urllib.request.build_opener(NoRedirect)

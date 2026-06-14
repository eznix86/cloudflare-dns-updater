from dataclasses import dataclass, field
from typing import Any

import yaml


@dataclass
class RecordConfig:
    name: str
    type: str = "A"
    ttl: int = 120
    proxied: bool = False


@dataclass
class ZoneConfig:
    zone: str
    records: list[RecordConfig] = field(default_factory=list)


@dataclass
class AppConfig:
    token_env: str = "CLOUDFLARE_TOKEN"
    ip_sources: list[str] = field(
        default_factory=lambda: [
            "https://ifconfig.me/ip",
            "https://api.ipify.org",
            "https://checkip.amazonaws.com",
            "https://icanhazip.com",
            "https://ipinfo.io/ip",
        ]
    )
    zones: list[ZoneConfig] = field(default_factory=list)
    dry_run: bool = False


def load_config(path: str) -> AppConfig:
    with open(path) as f:
        raw = yaml.safe_load(f)

    zones = []
    for z in raw.get("zones") or []:
        records = [RecordConfig(**r) for r in z.get("records") or []]
        zones.append(ZoneConfig(zone=z["zone"], records=records))

    kw: dict[str, Any] = {"zones": zones}
    if "token_env" in raw:
        kw["token_env"] = raw["token_env"]
    if "ip_sources" in raw and raw["ip_sources"] is not None:
        kw["ip_sources"] = raw["ip_sources"]
    if "dry_run" in raw:
        kw["dry_run"] = raw["dry_run"]
    return AppConfig(**kw)

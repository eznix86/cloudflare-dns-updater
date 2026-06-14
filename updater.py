import httpx
import structlog

from cloudflare_client import DNSClient
from config import AppConfig

logger = structlog.get_logger()


def get_public_ip(sources: list[str], *, client: httpx.Client | None = None) -> str | None:
    c = client or httpx.Client()
    for source in sources:
        try:
            resp = c.get(source, timeout=10)
            ip = resp.text.strip()
            if ip:
                logger.info("Got public IP", source=source, public_ip=ip)
                return ip
        except Exception:
            continue
    return None


def resolve_zone(client: DNSClient, zone_name: str) -> str | None:
    logger.info("Looking up zone", zone=zone_name)
    zone_id = client.get_zone_id(zone_name)
    if not zone_id:
        logger.error("Zone not found", zone=zone_name)
        return None
    return zone_id


def sync_record(client: DNSClient, zone_id: str, record, ip: str):
    records = client.list_records(zone_id=zone_id, name=record.name, type=record.type)
    existing = records[0] if records else None

    if existing:
        if existing["content"] == ip:
            logger.info("IP unchanged, skipping", record=record.name, public_ip=ip, record_id=existing["id"])
            return
        logger.info("Updating DNS record", record_id=existing["id"], old_ip=existing["content"], new_ip=ip)
        client.update_record(
            zone_id=zone_id,
            record_id=existing["id"],
            data={
                "type": record.type,
                "name": record.name,
                "content": ip,
                "ttl": record.ttl,
                "proxied": record.proxied,
            },
        )
    else:
        logger.info("Creating DNS record", record=record.name, public_ip=ip)
        client.create_record(
            zone_id=zone_id,
            data={
                "type": record.type,
                "name": record.name,
                "content": ip,
                "ttl": record.ttl,
                "proxied": record.proxied,
            },
        )


def sync_all_zones(client: DNSClient, config: AppConfig, ip: str):
    for zone_cfg in config.zones:
        zone_id = resolve_zone(client, zone_cfg.zone)
        if not zone_id:
            continue
        for record in zone_cfg.records:
            sync_record(client, zone_id, record, ip)

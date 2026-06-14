from config import load_config


def test_load_config(sample_config_path):
    cfg = load_config(str(sample_config_path))
    assert cfg.token_env == "MY_TOKEN"
    assert cfg.ip_sources == ["https://example.com/ip"]
    assert len(cfg.zones) == 1
    assert cfg.zones[0].zone == "example.com"
    assert len(cfg.zones[0].records) == 2
    assert cfg.zones[0].records[0].name == "@"
    assert cfg.zones[0].records[0].ttl == 300
    assert cfg.zones[0].records[0].proxied is True
    assert cfg.zones[0].records[1].name == "www"
    assert cfg.zones[0].records[1].type == "A"
    assert cfg.zones[0].records[1].ttl == 120
    assert cfg.zones[0].records[1].proxied is False


def test_load_config_defaults(tmp_path):
    p = tmp_path / "empty.yaml"
    p.write_text("zones: []")
    cfg = load_config(str(p))
    assert cfg.token_env == "CLOUDFLARE_TOKEN"
    assert len(cfg.ip_sources) > 0
    assert cfg.zones == []

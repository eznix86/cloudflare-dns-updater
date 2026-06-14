from pathlib import Path

import pytest


@pytest.fixture
def sample_config_path(tmp_path: Path) -> Path:
    p = tmp_path / "config.yaml"
    p.write_text("""token_env: MY_TOKEN
ip_sources:
  - https://example.com/ip
zones:
  - zone: example.com
    records:
      - name: "@"
        type: A
        ttl: 300
        proxied: true
      - name: www
        type: A
""")
    return p

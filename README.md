# tunnel
> bespoke reverse tunnel relay for TNS SMP

TNS SMP를 위한 리버스 터널링 도구입니다. 오라클 클라우드 E2 Micro instance를 중계 서버(Relay)로 사용하여, 공인 (Public) IPv4가 없는 로컬 환경의 마인크래프트 서버를 외부로 노출합니다.

## 설계

포트 포워딩 대신 서버 호스트가 중계 서버로 먼저 연결을 맺는 방식을 사용합니다.

```mermaid
graph LR
    Player["플레이어 (Client)"] -- "TCP:25565" --> Relay["오라클 클라우드 (Relay)"]
    Relay -- "Yamux Tunnel (TCP:8080)" --> Host["로컬 서버 (Host)"]
    Host -- "TCP:25565" --> MC["마인크래프트 (Target)"]
```

### 네트워크 흐름

1.  **Relay:** 오라클 VPS에서 실행됩니다.
    *   `8080`: 호스트(Host)와의 제어 채널 연결용
    *   `25565`: 플레이어들의 게임 접속용
2.  **Host (TNS):** 마인크래프트 서버가 돌아가는 로컬 머신에서 실행됩니다.
    *   시작 시 Relay의 `8080` 포트로 outbound TCP 연결을 맺습니다.

3.  **Multiplexing w/ Yamux:**
    *   Relay와 Host 간의 TCP 연결 위에 `hashicorp/yamux`를 사용하여 멀티플렉싱 세션을 생성합니다.
    *   플레이어가 Relay의 `25565` 포트로 접속하면, Relay는 기존에 맺어진 세션 내부에 가상의 ㄴtream을 생성하여 Host로 데이터를 전달합니다.
    *   Host는 이 스트림을 수락해 로컬의 `localhost:25565`로 트래픽을 프록시합니다.

## 오라클 클라우드 설정 (Oracle Cloud Setup)

이 프로젝트는 오라클 클라우드 환경을 기준으로 구성되었습니다.


* VCN Security List:

Ingress Rules에 `8080` (Tunnel Control) 및 `25565` (Minecraft) 포트 허용 추가해야 합니다.

* OS 방화벽
    *   인스턴스 내부 방화벽에서도 해당 포트를 열어줘야 합니다.
    ```bash
    # Oracle Linux 예시
    sudo firewall-cmd --permanent --add-port=8080/tcp
    sudo firewall-cmd --permanent --add-port=25565/tcp
    sudo firewall-cmd --reload
    ```

## deployment

Go 1.21 이상이 필요합니다.

### 빌드

```bash
make
# bin/tunnel-server 와 bin/tunnel-client 가 생성됩니다.
```



used
- `hashicorp/yamux`
- `charmbracelet/bubbletea`
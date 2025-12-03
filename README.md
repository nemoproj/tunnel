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

### 서버 실행 방법

#### 옵션 1: 데몬 모드 (권장 - 백그라운드 실행)

서버를 백그라운드에서 실행하고 TUI는 별도로 연결:

```bash
# 서버 데몬 시작
./bin/tunnel-server -daemon -control-port 8080 -game-port 25565

# TUI로 서버 상태 확인 (언제든지 종료/재연결 가능)
./bin/tunnel-server
```

TUI를 종료해도 서버는 계속 실행됩니다.

#### 옵션 2: TUI 내장 모드

TUI와 함께 서버 실행 (TUI 종료 시 서버도 종료):

```bash
# 데몬이 실행 중이지 않을 때 TUI를 실행하면 서버가 내장되어 시작됩니다
./bin/tunnel-server
```

#### systemd 서비스 설치 (선택사항)

systemd를 사용하여 서버를 시스템 서비스로 실행:

```bash
# 바이너리 복사
sudo cp bin/tunnel-server /usr/local/bin/

# 서비스 사용자 생성
sudo useradd -r -s /bin/false tunnel

# 서비스 파일 설치
sudo cp tunnel-server.service /etc/systemd/system/

# 서비스 시작 및 활성화
sudo systemctl daemon-reload
sudo systemctl enable tunnel-server
sudo systemctl start tunnel-server

# 상태 확인
sudo systemctl status tunnel-server

# TUI로 모니터링
./bin/tunnel-server
```

### 클라이언트 실행

```bash
./bin/tunnel-client
```

### 서버 데몬 제어

```bash
# 상태 확인
./bin/tunnel-server  # TUI로 연결

# 데몬 종료 (systemd 미사용 시)
pkill tunnel-server

# 데몬 종료 (systemd 사용 시)
sudo systemctl stop tunnel-server
```

used
- `hashicorp/yamux`
- `charmbracelet/bubbletea`

**Полноценный репозиторий с кодом сервиса есть на [GitHub](https://github.com/bonkkong/expert-octo-palm-tree)**

### Архитектура решения

- **Хост**: macOS (мой макбук).
- **Гипервизор**: UTM (QEMU).
- **Гость**: Rocky Linux (минимальная установка).
- **Доставка микросервиса**: Ansible по SSH.
- **Микросервис**: Go-бинарник (скомпилирован **на macOS**, на ВМ компилятор **не ставим**); HTTP на `127.0.0.1:8080`; экспортирует Prometheus метрики, включая тип окружения (VM/container/bare-metal).
- **Сервис-менеджер**: systemd юнит; автозапуск; работает в фоне.

---
### Решение

#### 0. Prerequisites:
- Git, Go, Ansible — установлены на хосте (или машине/контейнере с которой выполняем развёртывание).
- Среда для виртуальной машины: UTM (но тестирование быстрее и проще всего выполнить в orbstack).
- .iso образ Rocky Linux (в случае использования UTM).

#### 1. Подготовка

1) Скачиваем Rocky Linux 9.6 Minimal ISO (aarch64); Запускаемся и устанавливаем.
2) Выполняем базовую минимальную настройку: создаём пользователя `ansible`, добавляем в группу `wheel`, настраиваем sudo без пароля, копируем ssh ключи.
3) Проверяем соединение `ssh ansible@<ip>` из хоста.
4) На хосте создаём рабочую папку проекта:

```
repo/
├─ ansible.cfg
├─ inventories/
│  └─ hosts.yml
└─ ping.yml
```

**`ansible.cfg`:**

```
[defaults]
inventory = inventories/hosts.yml
host_key_checking = False
retry_files_enabled = False
stdout_callback = yaml
```

**`inventories/hosts.yml`:**

```yaml
all:
  hosts:
    rocky-vm:
      ansible_host: rocky.orb.local
      ansible_port: 22
      ansible_user: ansible
      ansible_become: true
      ansible_ssh_private_key_file: ~/.orbstack/ssh/id_ed25519
```

> В данном примере для удобства тестирования подключаемся к контейнеру в orbstack, но с UTM работает так же.

**`ping.yml` (smoke-тест):**

```yaml
- hosts: all
  gather_facts: false
  tasks:
    - name: Ping
      ansible.builtin.ping:
```

Для проверки корректной работы выполняем: `ansible-playbook ping.yml`
Ожидаемо — `ok: [rocky-vm]`.

#### 2) Написание сервиса и проверка работы

> Код main.go остался за кадром, но есть в репо на [GitHub](https://github.com/bonkkong/expert-octo-palm-tree)

1. Инициализируем модуль и подтягиваем зависимость Prometheus:

```bash
go mod init example.com/metrixd
go get github.com/prometheus/client_golang@latest
go mod tidy
```

2. Локальная сборка на хосте для теста:

```bash
go build -ldflags="-s -w" -o metrixd .
./metrixd -listen-address=127.0.0.1:8080 

curl -s http://localhost:8080 | grep host_environment_info
```

3. Кросс-сборка для Rocky:

```bash
GOOS=linux
GOARCH=arm64
CGO_ENABLED=0
go build -ldflags="-s -w" -o metrixd_linux_arm64 .
```

4. Запуск на ВМ :

```bash
./metrixd_linux_arm64 -listen-address=127.0.0.1:8080

curl -s http://localhost:8080 | grep host_environment_info

# Должно вывести, например: # host_environment_info{type="vm"} 1
```

#### 3) Доставляем сервис на машину и запускаем

1. На хосте дорабатываем структуру репозитория: создаём готовую роль Ansible в каталоге `roles/metrixd` (файлы `defaults`, `handlers`, `tasks`, `templates`, а также кладём бинарник):z

```
repo/
├─ ansible.cfg
├─ inventories/
│  └─ hosts.yml
├─ site.yml
├─ roles/
│  └─ metrixd/
│     ├─ defaults/main.yml
│     ├─ files/               # сюда положим собранный бинарник metrixd
│     │  └─ metrixd
│     ├─ handlers/main.yml
│     ├─ tasks/main.yml
│     └─ templates/metrixd.service.j2
└─ Makefile
```

2. Создаём ansible роль для сервиса.

**`defaults/main.yml`** (собираем переменные в одном месте):

```yaml
metrixd_user: metrix
metrixd_group: metrix
metrixd_install_dir: /opt/metrixd
metrixd_binary_name: metrixd
metrixd_listen_addr: 127.0.0.1:8080
```

**`tasks/main.yml`** (базовый минимум действий):

```yaml
- name: Ensure group
  ansible.builtin.group:
    name: "{{ metrixd_group }}"

- name: Ensure user
  ansible.builtin.user:
    name: "{{ metrixd_user }}"
    group: "{{ metrixd_group }}"
    shell: /usr/sbin/nologin
    create_home: false
    system: true

- name: Create install dir
  ansible.builtin.file:
    path: "{{ metrixd_install_dir }}"
    state: directory
    owner: "{{ metrixd_user }}"
    group: "{{ metrixd_group }}"
    mode: '0755'

- name: Copy binary
  ansible.builtin.copy:
    src: "{{ metrixd_binary_name }}"
    dest: "{{ metrixd_install_dir }}/{{ metrixd_binary_name }}"
    owner: "{{ metrixd_user }}"
    group: "{{ metrixd_group }}"
    mode: '0755'
  notify: restart metrixd

- name: Install systemd unit
  ansible.builtin.template:
    src: metrixd.service.j2
    dest: /etc/systemd/system/metrixd.service
    mode: '0644'
  notify:
    - daemon-reload
    - restart metrixd

- name: Enable & start
  ansible.builtin.systemd:
    name: metrixd
    enabled: true
    state: started

# Быстрая проверка доступности метрик
- name: Wait HTTP 200 from localhost
  ansible.builtin.uri:
    url: "http://127.0.0.1:8080"
    status_code: 200
  register: http_check
  retries: 10
  delay: 1
  until: http_check.status == 200

```

**`handlers/main.yml`**

```yaml
- name: daemon-reload
  ansible.builtin.systemd:
    daemon_reload: true

- name: restart metrixd
  ansible.builtin.systemd:
    name: metrixd
    state: restarted
```

3. Создаём шаблон systemd юнита.

```
[Unit]
Description=MetrixD - minimal Prometheus exporter with host type
After=network-online.target
Wants=network-online.target

[Service]
User={{ metrixd_user }}
Group={{ metrixd_group }}
ExecStart={{ metrixd_install_dir }}/{{ metrixd_binary_name }} --listen-address={{ metrixd_listen_addr }}
Restart=on-failure
RestartSec=2

# Hardening (совместимо с Rocky 9):
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ProtectClock=true
ProtectHostname=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
MemoryDenyWriteExecute=true
CapabilityBoundingSet=
AmbientCapabilities=
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

2. Запускаем развёртывание командой `ansible-playbook -i inventories/hosts.yml site.yml` из корня репозитория.

**Результат:** Ansible копирует бинарник, устанавливает systemd‑unit, перезапускает службу и проверяет, что метрика доступна на `http://localhost:8080`.

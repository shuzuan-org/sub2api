# 119 上以容器跑，加入 ~/server-prometheus 的 compose（见 README）。
FROM python:3.12-slim
# psycopg 只在配置了 TRIAGE_PG_DSN（ops_error_logs 取证）时用到；其余全是标准库。
RUN pip install --no-cache-dir "psycopg[binary]==3.2.3"
WORKDIR /app
COPY triage.py rules.json ./
# 以 nobody(65534) 运行：和 Alertmanager 同一个 uid，可以直接只读挂它的 secrets/smtp_password，不用再复制一份密码。
RUN mkdir /data && chown 65534:65534 /data
USER 65534:65534
ENV TRIAGE_DB=/data/triage.db PYTHONUNBUFFERED=1
EXPOSE 9099
HEALTHCHECK --interval=30s --timeout=3s CMD python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:9099/healthz', timeout=2)"
CMD ["python3", "triage.py"]

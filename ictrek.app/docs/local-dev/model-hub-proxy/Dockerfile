FROM python:3.12.14-slim-trixie

ENV PYTHONUNBUFFERED=1

WORKDIR /app

COPY proxy.py /app/proxy.py

EXPOSE 11535 11536

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=6 \
    CMD ["python3", "/app/proxy.py", "--health"]

ENTRYPOINT ["python3", "/app/proxy.py"]

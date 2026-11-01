FROM python:3.10-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    MPLBACKEND=Agg

# Runtime executes without network access. Install dependencies at image build
# time so submitted code cannot download packages.
RUN pip install --no-cache-dir --disable-pip-version-check \
    numpy==2.2.6 \
    matplotlib==3.10.3 \
    && rm -rf /root/.cache

WORKDIR /tmp
CMD ["python", "-I"]

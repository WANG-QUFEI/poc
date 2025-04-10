-- migrate:up
CREATE TABLE
    if NOT EXISTS device_types (
        id serial PRIMARY key,
        name text NOT NULL UNIQUE,
        description text,
        created_at timestamptz NOT NULL DEFAULT now (),
        deleted_at timestamptz
    );

CREATE index if NOT EXISTS idx_device_types_deleted_at ON device_types (deleted_at);

CREATE TABLE
    if NOT EXISTS devices (
        id serial PRIMARY key,
        device_id text NOT NULL UNIQUE,
        device_type text NOT NULL REFERENCES device_types (name),
        hostname text NOT NULL,
        protocols text ARRAY NOT NULL,
        rest_port INT,
        rest_path text,
        gRpc_port INT,
        polling_status text,
        created_at timestamptz NOT NULL DEFAULT now (),
        last_checked_at timestamptz,
        deleted_at timestamptz,
        CONSTRAINT unique_hostname_rest_port UNIQUE (hostname, rest_port),
        CONSTRAINT unique_hostname_gRpc_port UNIQUE (hostname, gRpc_port)
    );

CREATE index if NOT EXISTS idx_devices_device_type ON devices (device_type);

CREATE index if NOT EXISTS idx_devices_hostname ON devices (hostname);

CREATE index if NOT EXISTS idx_devices_created_at ON devices (created_at);

CREATE index if NOT EXISTS idx_devices_last_checked_at ON devices (last_checked_at);

CREATE index if NOT EXISTS idx_devices_deleted_at ON devices (deleted_at);

CREATE index if NOT EXISTS idx_devices_poll_status_last_checked_at ON devices (polling_status, last_checked_at);

CREATE TABLE
    if NOT EXISTS polling_history (
        id serial PRIMARY key,
        device_id text NOT NULL REFERENCES devices (device_id),
        hw_version text,
        sw_version text,
        fw_version text,
        device_status text,
        device_checksum text,
        polling_result text NOT NULL,
        failure_reason text,
        created_at timestamptz NOT NULL DEFAULT now ()
    );

CREATE index if NOT EXISTS idx_polling_history_device_id ON polling_history (device_id);

CREATE index if NOT EXISTS idx_polling_history_created_at ON polling_history (created_at);

-- migrate:down
SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: device_types; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.device_types (
    id integer NOT NULL,
    name text NOT NULL,
    description text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


--
-- Name: device_types_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.device_types_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: device_types_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.device_types_id_seq OWNED BY public.device_types.id;


--
-- Name: devices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.devices (
    id integer NOT NULL,
    device_id text NOT NULL,
    device_type text NOT NULL,
    hostname text NOT NULL,
    protocols text[] NOT NULL,
    rest_port integer,
    rest_path text,
    grpc_port integer,
    polling_status text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_checked_at timestamp with time zone,
    deleted_at timestamp with time zone
);


--
-- Name: devices_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.devices_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: devices_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.devices_id_seq OWNED BY public.devices.id;


--
-- Name: polling_history; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.polling_history (
    id integer NOT NULL,
    device_id text NOT NULL,
    hw_version text,
    sw_version text,
    fw_version text,
    device_status text,
    device_checksum text,
    polling_result text NOT NULL,
    failure_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: polling_history_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.polling_history_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: polling_history_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.polling_history_id_seq OWNED BY public.polling_history.id;


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version character varying(128) NOT NULL
);


--
-- Name: device_types id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.device_types ALTER COLUMN id SET DEFAULT nextval('public.device_types_id_seq'::regclass);


--
-- Name: devices id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices ALTER COLUMN id SET DEFAULT nextval('public.devices_id_seq'::regclass);


--
-- Name: polling_history id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.polling_history ALTER COLUMN id SET DEFAULT nextval('public.polling_history_id_seq'::regclass);


--
-- Name: device_types device_types_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.device_types
    ADD CONSTRAINT device_types_name_key UNIQUE (name);


--
-- Name: device_types device_types_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.device_types
    ADD CONSTRAINT device_types_pkey PRIMARY KEY (id);


--
-- Name: devices devices_device_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_device_id_key UNIQUE (device_id);


--
-- Name: devices devices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_pkey PRIMARY KEY (id);


--
-- Name: polling_history polling_history_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.polling_history
    ADD CONSTRAINT polling_history_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: devices unique_hostname_grpc_port; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT unique_hostname_grpc_port UNIQUE (hostname, grpc_port);


--
-- Name: devices unique_hostname_rest_port; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT unique_hostname_rest_port UNIQUE (hostname, rest_port);


--
-- Name: idx_device_types_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_device_types_deleted_at ON public.device_types USING btree (deleted_at);


--
-- Name: idx_devices_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_created_at ON public.devices USING btree (created_at);


--
-- Name: idx_devices_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_deleted_at ON public.devices USING btree (deleted_at);


--
-- Name: idx_devices_device_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_device_type ON public.devices USING btree (device_type);


--
-- Name: idx_devices_hostname; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_hostname ON public.devices USING btree (hostname);


--
-- Name: idx_devices_last_checked_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_last_checked_at ON public.devices USING btree (last_checked_at);


--
-- Name: idx_devices_poll_status_last_checked_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_devices_poll_status_last_checked_at ON public.devices USING btree (polling_status, last_checked_at);


--
-- Name: idx_polling_history_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_polling_history_created_at ON public.polling_history USING btree (created_at);


--
-- Name: idx_polling_history_device_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_polling_history_device_id ON public.polling_history USING btree (device_id);


--
-- Name: devices devices_device_type_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_device_type_fkey FOREIGN KEY (device_type) REFERENCES public.device_types(name);


--
-- Name: polling_history polling_history_device_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.polling_history
    ADD CONSTRAINT polling_history_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(device_id);


--
-- PostgreSQL database dump complete
--


--
-- Dbmate schema migrations
--

INSERT INTO public.schema_migrations (version) VALUES
    ('20250408170630');

CREATE TABLE memory_audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    action text NOT NULL CHECK (action IN ('remember', 'supersede', 'forget')),
    memory_id text NOT NULL REFERENCES memories(id),
    actor text NOT NULL DEFAULT '',
    source text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX memory_audit_events_memory_idx
    ON memory_audit_events (memory_id, sequence DESC);
CREATE INDEX memory_audit_events_actor_idx
    ON memory_audit_events (actor, sequence DESC);

CREATE FUNCTION reject_memory_audit_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'memory audit events are append-only';
END;
$$;

CREATE TRIGGER memory_audit_events_append_only
BEFORE UPDATE OR DELETE ON memory_audit_events
FOR EACH ROW EXECUTE FUNCTION reject_memory_audit_change();

-- What happened to each captured request after it arrived: the forward
-- target's reply and the response sent back to the caller. Both are filled in
-- after the insert (an async forward finishes later), so they start NULL.
ALTER TABLE requests ADD COLUMN forward_result JSONB;
ALTER TABLE requests ADD COLUMN response_info JSONB;

-- 準備の段階と人による停止を、同じ task_runs に記録する（issue #27）。
ALTER TABLE task_runs ADD COLUMN phase TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs DROP CONSTRAINT task_runs_status_check;
ALTER TABLE task_runs ADD CONSTRAINT task_runs_status_check
    CHECK (status IN ('running', 'done', 'failed', 'interrupted', 'cancelled'));

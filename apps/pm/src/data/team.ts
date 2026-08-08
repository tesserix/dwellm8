/**
 * The firm's own team (#353): the sub-managers it employs, what each carries
 * and who is responsible for which building.
 *
 * The cap is the reason this screen exists, so it is computed here and shown
 * before anybody is assigned — a 409 after the tap is a worse way to learn it.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type {
  OpsEmployStaffRequest, OpsStaffAssignment, OpsStaffMember, OpsStaffRole,
} from '@dwellm8/mobile-shared';

/** One colleague, with what they hold and what they may still take. */
export type Colleague = OpsStaffMember & {
  properties: OpsStaffAssignment[];
  spare: number;
  atCapacity: boolean;
};

export type TeamBoard = {
  loading: boolean;
  error?: string;
  roles: OpsStaffRole[];
  working: Colleague[];
  gone: Colleague[];
  employ: (req: OpsEmployStaffRequest) => Promise<void>;
  saveRole: (role: { name: string; permissions: string[]; property_limit: number }) => Promise<void>;
  assign: (staffId: string, propertyId: string) => Promise<void>;
  release: (assignmentId: string) => Promise<void>;
  exit: (staffId: string, on: string) => Promise<void>;
  setLimit: (staffId: string, limit: number) => Promise<void>;
  reload: () => void;
};

// The cap turns on the server's own count, not on the rows this screen happens
// to be showing — the trigger that enforces it counts the same way.
function withLoad(m: OpsStaffMember, assignments: OpsStaffAssignment[]): Colleague {
  const spare = Math.max(0, m.property_limit - m.held);
  return {
    ...m,
    properties: assignments.filter((a) => a.staff_id === m.id),
    spare,
    atCapacity: spare === 0,
  };
}

export function useTeam(): TeamBoard {
  const api = useMemo(() => apiFromEnv(), []);
  const [roles, setRoles] = useState<OpsStaffRole[]>([]);
  const [members, setMembers] = useState<OpsStaffMember[]>([]);
  const [assignments, setAssignments] = useState<OpsStaffAssignment[]>([]);
  const [loading, setLoading] = useState(Boolean(api));
  const [error, setError] = useState<string | undefined>();
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    api.opsTeam()
      .then((got) => {
        if (!alive) return;
        setRoles(got.roles);
        setMembers(got.team);
        setAssignments(got.assignments);
        setError(undefined);
      })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  const colleagues = useMemo(
    () => members.map((m) => withLoad(m, assignments)), [members, assignments]);

  const client = useCallback(() => {
    if (!api) throw new Error('The API is not configured on this build.');
    return api;
  }, [api]);

  const employ = useCallback(async (req: OpsEmployStaffRequest) => {
    await client().opsEmployStaff(req);
    reload();
  }, [client, reload]);

  const saveRole = useCallback(async (
    role: { name: string; permissions: string[]; property_limit: number },
  ) => {
    await client().opsSaveStaffRole(role);
    reload();
  }, [client, reload]);

  const assign = useCallback(async (staffId: string, propertyId: string) => {
    const who = colleagues.find((c) => c.id === staffId);
    if (who?.atCapacity) {
      throw new Error(
        `${who.full_name} already holds as many properties as their role allows — `
        + 'hand one back, or raise what they carry.');
    }
    await client().opsAssignProperty(staffId, propertyId);
    reload();
  }, [client, colleagues, reload]);

  const release = useCallback(async (assignmentId: string) => {
    await client().opsReleaseAssignment(assignmentId);
    reload();
  }, [client, reload]);

  const exit = useCallback(async (staffId: string, on: string) => {
    await client().opsUpdateStaff(staffId, { state: 'exited', exited_on: on });
    reload();
  }, [client, reload]);

  const setLimit = useCallback(async (staffId: string, limit: number) => {
    await client().opsUpdateStaff(staffId, { property_limit: limit });
    reload();
  }, [client, reload]);

  return {
    loading,
    error,
    roles,
    working: colleagues.filter((c) => c.state !== 'exited'),
    gone: colleagues.filter((c) => c.state === 'exited'),
    employ,
    saveRole,
    assign,
    release,
    exit,
    setLimit,
    reload,
  };
}

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Text, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, ListRow, StatusPill, Field, Button, ActionBar, Toast,
  apiFromEnv, type RentalApplication, type Tone,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';

/**
 * The owner's application queue (#142). Review acknowledges, accept drafts the
 * tenancy with the application's terms carried over, decline records why —
 * because "no reply" is the thing applicants remember about renting.
 */

const tone: Record<string, Tone> = {
  submitted: 'amber', under_review: 'blue', accepted: 'green',
  declined: 'red', withdrawn: 'neutral',
};

export default function Applications() {
  const router = useRouter();
  const client = useMemo(() => apiFromEnv(), []);
  const [apps, setApps] = useState<RentalApplication[]>([]);
  const [loading, setLoading] = useState(!!client);
  const [selected, setSelected] = useState<RentalApplication | null>(null);
  const [toast, setToast] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [rent, setRent] = useState('');
  const [reason, setReason] = useState('');

  const load = useCallback(() => {
    if (!client) return;
    setLoading(true);
    client.applicationsQueue()
      .then((list) => setApps(list))
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [client]);
  useEffect(load, [load]);

  const act = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true); setError('');
    try {
      await fn();
      setToast(done);
      setSelected(null); setName(''); setPhone(''); setRent(''); setReason('');
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'That did not go through.');
    } finally {
      setBusy(false);
    }
  };

  if (selected && client) {
    const a = selected;
    return (
      <>
        <BackHeader title={a.headline ?? 'Application'} subtitle={`Move-in ${a.move_in} · ${a.term_months} months`} onBack={() => setSelected(null)} />
        <Screen>
          <Card>
            <StatusPill text={a.state.replace(/_/g, ' ')} tone={tone[a.state] ?? 'neutral'} />
            {a.offer_minor ? (
              <Text style={s.sub}>Offered {inr(a.offer_minor, { noPaise: true })} per month</Text>
            ) : null}
            {a.note ? <Text style={s.note}>{a.note}</Text> : null}
          </Card>
          <Card>
            <Text style={s.h}>Accept — draft the tenancy</Text>
            <Field label="Tenant's full name" value={name} onChange={setName} />
            <Field label="Tenant's mobile" value={phone} onChange={setPhone} keyboardType="phone-pad" />
            <Field label="Agreed rent, ₹ per month (blank keeps the offer)" value={rent} onChange={setRent} keyboardType="numeric" />
          </Card>
          <Card>
            <Text style={s.h}>Or decline, with the reason the applicant reads</Text>
            <Field label="Reason" value={reason} onChange={setReason} placeholder="Another applicant was accepted" />
          </Card>
          {error ? <Text style={s.err}>{error}</Text> : null}
        </Screen>
        <ActionBar>
          <Button
            label="Decline"
            tone="secondary"
            disabled={busy || reason.trim().length < 3}
            onPress={() => act(() => client.declineApplication(a.id, reason.trim()), 'Declined, with the reason on record')}
            style={{ flex: 1 }}
          />
          <Button
            label={busy ? 'Working…' : 'Accept'}
            disabled={busy || !name.trim() || phone.trim().length < 10}
            onPress={() => act(() => client.acceptApplication(a.id, {
              name: name.trim(), phone: phone.trim(),
              rentMinor: rent.trim() ? Math.round(Number(rent) * 100) : undefined,
            }), 'Accepted — the tenancy draft is ready')}
            style={{ flex: 1.4 }}
          />
        </ActionBar>
      </>
    );
  }

  return (
    <>
      <BackHeader title="Applications" subtitle="Everyone who has formally applied" onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {!client ? (
          <Card><Text style={s.note}>Demo mode — applications arrive here once the app points at a live API.</Text></Card>
        ) : null}
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {apps.map((a, i) => (
            <ListRow
              key={a.id}
              title={a.headline ?? 'Application'}
              subtitle={`Move-in ${a.move_in} · ${a.term_months} months${a.offer_minor ? ` · offers ${inr(a.offer_minor, { noPaise: true })}` : ''}`}
              meta={a.note}
              right={<StatusPill text={a.state.replace(/_/g, ' ')} tone={tone[a.state] ?? 'neutral'} />}
              onPress={() => {
                if (a.state === 'submitted') client?.reviewApplication(a.id).catch(() => {});
                setSelected(a);
              }}
              last={i === apps.length - 1}
            />
          ))}
          {!apps.length && !loading ? (
            <Text style={s.empty}>No applications yet — they appear the moment somebody applies.</Text>
          ) : null}
        </Card>
        {error ? <Text style={s.err}>{error}</Text> : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(2) },
  sub: { ...font.body, color: color.inkStrong, marginTop: space(3) },
  note: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 18 },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(6) },
  err: { ...font.small, color: '#B4232A', marginHorizontal: space(4), marginTop: space(2) },
});

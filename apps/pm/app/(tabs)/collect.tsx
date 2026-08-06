import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Segmented, SearchBar, ListRow, Avatar,
  StatusPill, Metric,
  apiFromEnv, color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { OpsArrear } from '@dwellm8/mobile-shared';

/**
 * Collections worklist — the live roster and what each tenancy owes today,
 * from ledger postings (GET /v1/ops/arrears). Mandate retries and
 * promise-to-pay tracking arrive with their own legs; nothing here stages
 * them.
 */

export default function Collect() {
  const router = useRouter();
  const api = useMemo(() => apiFromEnv(), []);
  const [tab, setTab] = useState('In arrears');
  const [q, setQ] = useState('');
  const [rows, setRows] = useState<OpsArrear[]>([]);
  const [loading, setLoading] = useState(!!api);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    api.opsArrears()
      .then((list) => { if (alive) { setRows(list); setLoading(false); } })
      .catch((err: Error) => { if (alive) { setError(err.message); setLoading(false); } });
    return () => { alive = false; };
  }, [api]);

  const list = useMemo(() => {
    const needle = q.toLowerCase();
    const filtered = rows.filter(
      (a) => !q
        || a.unit.toLowerCase().includes(needle)
        || a.property.toLowerCase().includes(needle)
        || (a.phone ?? '').includes(q),
    );
    const view = tab === 'In arrears' ? filtered.filter((a) => a.due_amount_minor > 0) : filtered;
    return view.slice().sort((a, b) => b.due_amount_minor - a.due_amount_minor);
  }, [rows, tab, q]);

  const outstanding = list.reduce((sum, a) => sum + Math.max(0, a.due_amount_minor), 0);
  const inArrears = rows.filter((a) => a.due_amount_minor > 0).length;

  return (
    <>
      <AppHeader
        title="Collections"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={{ marginTop: space(3) }}>
          <Segmented items={['In arrears', 'Whole roster']} value={tab} onChange={setTab} />
        </View>

        <View style={s.metrics}>
          <Metric value={loading ? '…' : inr(outstanding, { noPaise: true })} label="outstanding in view" tone={outstanding ? 'red' : 'green'} />
          <Metric value={loading ? '…' : String(inArrears)} label="tenancies in arrears" tone={inArrears ? 'amber' : 'green'} />
        </View>

        <View style={{ marginTop: space(3) }}>
          <SearchBar value={q} onChange={setQ} placeholder="Unit, property or phone" />
        </View>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? (
            <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View>
          ) : null}
          {error ? <Text style={s.empty}>{error}</Text> : null}
          {list.map((a, i) => (
            <ListRow
              key={a.lease_id}
              left={<Avatar initials={a.unit.slice(0, 2).toUpperCase()} tone={a.due_amount_minor > 0 ? 'red' : 'green'} />}
              title={`${a.unit}, ${a.property} — ${inr(Math.max(0, a.due_amount_minor), { noPaise: true })}`}
              subtitle={`${a.locality}${a.phone ? ` · ${a.phone}` : ''}`}
              meta={`rent ${inr(a.rent_amount_minor, { noPaise: true })} · as of ${a.as_of}`}
              right={
                <StatusPill
                  text={a.due_amount_minor > 0 ? 'Owes' : 'Clear'}
                  tone={a.due_amount_minor > 0 ? 'red' : 'green'}
                />
              }
              onPress={() => router.push(`/arrear?id=${a.lease_id}`)}
              last={i === list.length - 1}
            />
          ))}
          {!loading && !error && !list.length ? (
            <Text style={s.empty}>
              {tab === 'In arrears' ? 'Nobody owes anything. Good.' : 'No live tenancies on this portfolio yet.'}
            </Text>
          ) : null}
        </Card>

        <Card>
          <Text style={s.helpTitle}>Recording payments</Text>
          <Text style={s.helpBody}>
            A tenant's UPI payment posts to the ledger and issues a receipt the moment it confirms.
            Cash, a cheque or a transfer you have seen land is yours to record — tap a row, and the
            receipt posts to the same ledger the tenant reads.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(3), marginBottom: space(1) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  helpTitle: { ...font.h3, color: color.inkStrong },
  helpBody: { ...font.body, color: color.inkSoft, marginTop: 6, lineHeight: 21 },
});

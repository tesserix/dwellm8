import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useRouter, type Href } from 'expo-router';
import {
  AppHeader, AvatarButton, Button, Card, Screen, SearchBar, ListRow, Metric,
  BuildingIcon, PlusIcon,
  apiFromEnv, color, font, space,
} from '@dwellm8/mobile-shared';
import type { OpsProperty } from '@dwellm8/mobile-shared';

/**
 * Portfolio — the properties under this scope (GET /v1/ops/properties): the
 * firm's own, or one owner's under the mandate the switcher picked.
 * Per-unit tenancy state lives on Collections, where the money question is.
 */

export default function Portfolio() {
  const router = useRouter();
  const api = useMemo(() => apiFromEnv(), []);
  const [q, setQ] = useState('');
  const [rows, setRows] = useState<OpsProperty[]>([]);
  const [loading, setLoading] = useState(!!api);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    api.opsProperties()
      .then((list) => { if (alive) { setRows(list); setLoading(false); } })
      .catch((err: Error) => { if (alive) { setError(err.message); setLoading(false); } });
    return () => { alive = false; };
  }, [api]);

  const list = useMemo(() => {
    const needle = q.toLowerCase();
    return rows.filter(
      (p) => !q
        || p.name.toLowerCase().includes(needle)
        || p.locality.toLowerCase().includes(needle)
        || p.city.toLowerCase().includes(needle),
    );
  }, [rows, q]);

  const units = rows.reduce((a, p) => a + p.unit_count, 0);

  return (
    <>
      <AppHeader
        title="Portfolio"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      <Screen>
        <View style={s.metrics}>
          <Metric value={loading ? '…' : String(rows.length)} label="properties" tone="blue" />
          <Metric value={loading ? '…' : String(units)} label="units managed" tone="green" />
        </View>

        <Button
          label="Onboard a new owner"
          icon={<PlusIcon size={19} c="#FFF" />}
          onPress={() => router.push('/onboard' as Href)}
          style={{ marginHorizontal: space(4), marginTop: space(3) }}
        />

        <SearchBar value={q} onChange={setQ} placeholder="Property, locality or city" />

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? (
            <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View>
          ) : null}
          {error ? <Text style={s.empty}>{error}</Text> : null}
          {list.map((p, i) => (
            <ListRow
              key={p.id}
              left={<BuildingIcon size={22} c={color.accent} />}
              title={p.name}
              subtitle={`${p.locality ? `${p.locality}, ` : ''}${p.city}`}
              meta={`${p.kind} · ${p.unit_count} unit${p.unit_count === 1 ? '' : 's'} · ${p.code}`}
              onPress={() => {}}
              last={i === list.length - 1}
            />
          ))}
          {!loading && !error && !list.length ? (
            <Text style={s.empty}>
              Nothing under this scope yet — onboard an owner, or switch portfolios from the Today
              screen's title.
            </Text>
          ) : null}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  metrics: { flexDirection: 'row', gap: 10, marginHorizontal: space(4), marginTop: space(4) },
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
});

import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Screen, KeyValue, StatusPill, Toast,
  apiFromEnv, color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { refreshWorklists, useOpsChecklist } from '../src/data/worklists';

/** One checklist: settle steps, in order, blockers first (ADR-0032). */

const taskTone: Record<string, Tone> = {
  pending: 'blue', blocked: 'neutral', done: 'green', skipped: 'amber',
};

export default function Checklist() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id } = useLocalSearchParams<{ id?: string }>();
  const { loading, error, data: c } = useOpsChecklist(id);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const act = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    try {
      await fn();
      refreshWorklists();
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (loading || !c) {
    return (
      <>
        <BackHeader title="Process" onBack={goBack} />
        <Screen>
          <View style={{ paddingVertical: space(10), alignItems: 'center' }}>
            {loading ? <ActivityIndicator /> : <Text style={s.sub}>{error ?? 'No such process.'}</Text>}
          </View>
        </Screen>
      </>
    );
  }

  const api = apiFromEnv();
  const open = c.state === 'open';
  const blocking = (c.blocking_outstanding ?? []).length;

  return (
    <>
      <BackHeader
        title={c.process.replace(/_/g, ' ').replace(/^\w/, (ch) => ch.toUpperCase())}
        subtitle={`anchored ${c.anchor_on}`}
        onBack={goBack}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        <Card>
          <StatusPill text={c.state} tone={c.state === 'open' ? 'blue' : c.state === 'completed' ? 'green' : 'neutral'} dot />
          <KeyValue k="Steps" v={`${c.tasks.filter((t) => t.state === 'done' || t.state === 'skipped').length}/${c.tasks.length} settled`} />
          <KeyValue k="Blocking outstanding" v={String(blocking)} tone={blocking ? 'red' : 'green'} last />
        </Card>

        {c.tasks.map((t) => (
          <Card key={t.id}>
            <View style={s.row}>
              <View style={{ flex: 1 }}>
                <Text style={s.taskTitle}>{t.title}</Text>
                <Text style={s.sub}>
                  {t.owner_role} · due {t.due_on}{t.blocking ? ' · blocking' : ''}
                </Text>
              </View>
              <StatusPill text={t.state} tone={taskTone[t.state] ?? 'neutral'} />
            </View>
            {open && t.state === 'pending' && api ? (
              <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                <Button label="Done" small onPress={() => act(() => api.opsChecklistComplete(c.id, t.step_code))} disabled={busy} style={{ flex: 1 }} />
                {!t.blocking ? (
                  <Button label="Skip" tone="secondary" small onPress={() => act(() => api.opsChecklistSkip(c.id, t.step_code, 'not applicable'))} disabled={busy} style={{ flex: 1 }} />
                ) : null}
              </View>
            ) : null}
          </Card>
        ))}

        {open && api ? (
          <Button
            label={blocking ? `Finish — ${blocking} blocking step${blocking === 1 ? '' : 's'} outstanding` : busy ? 'Finishing…' : 'Finish the process'}
            onPress={() => act(() => api.opsChecklistFinish(c.id))}
            disabled={busy || blocking > 0}
            style={{ marginHorizontal: space(4), marginBottom: space(4) }}
          />
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  taskTitle: { ...font.title, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
});

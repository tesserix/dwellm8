import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Button, Card, Field, Screen, SectionTitle, StatusPill, ListRow,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import {
  DAY_INITIALS, DEFAULT_ZONE, addSeries, atLocalTime, cancelViewing, endSeries,
  fmtOccurrence, moveViewing, seriesShape, seriesWhen, useListingViewings,
} from '../src/data/viewings';

/**
 * One listing's viewing times (#330, #333): the recurring series, the times
 * they produced, and the two exceptions a manager actually needs — call off
 * this one, or move this one.
 */

const slotTone: Record<string, Tone> = {
  open: 'green', closed: 'neutral', cancelled: 'red', done: 'blue',
};

const today = () => new Date().toISOString().slice(0, 10);

export default function ViewingTimes() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { id, headline } = useLocalSearchParams<{ id?: string; headline?: string }>();
  const { loading, error, schedules, slots } = useListingViewings(id);

  const [adding, setAdding] = useState(false);
  const [days, setDays] = useState<number[]>([]);
  const [time, setTime] = useState('10:00');
  const [mins, setMins] = useState('30');
  const [people, setPeople] = useState('4');
  const [point, setPoint] = useState('');
  const [from, setFrom] = useState(today());

  const [open, setOpen] = useState<string | null>(null);
  const [moveTo, setMoveTo] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | undefined>();

  const toggle = (d: number) =>
    setDays((p) => (p.includes(d) ? p.filter((x) => x !== d) : [...p, d]));

  const run = async (what: () => Promise<void>) => {
    setBusy(true);
    setFailed(undefined);
    try {
      await what();
      setAdding(false);
      setOpen(null);
    } catch (e) {
      setFailed((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const publish = () => run(async () => {
    if (!id) throw new Error('No listing was named.');
    await addSeries(id, {
      weekdays: days, start_time: time, duration_mins: Number(mins) || 30,
      capacity: Number(people) || 4, meeting_point: point || undefined, starts_on: from,
    });
    setDays([]);
    setPoint('');
  });

  // Keep the day, take the new wall clock at the property — moving a viewing is
  // almost always "same Saturday, an hour later".
  const move = (slotId: string, startsAt: string, zone: string) => run(async () => {
    await moveViewing(slotId, atLocalTime(startsAt, moveTo, zone));
    setMoveTo('');
  });

  return (
    <>
      <BackHeader
        title="Viewing times"
        subtitle={headline || 'This listing'}
        onBack={goBack}
      />
      <Screen>
        {failed ? <Text style={s.failed}>{failed}</Text> : null}

        <SectionTitle>Recurring times</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {loading ? (
            <View style={{ paddingVertical: space(6), alignItems: 'center' }}><ActivityIndicator /></View>
          ) : null}
          {error ? <Text style={s.empty}>{error}</Text> : null}
          {schedules.map((sc, i) => (
            <View key={sc.id}>
              <ListRow
                title={seriesWhen(sc)}
                subtitle={sc.meeting_point || 'Meeting point not set'}
                meta={seriesShape(sc)}
                right={<StatusPill text={sc.state} tone={sc.state === 'active' ? 'green' : 'neutral'} />}
                onPress={() => setOpen(open === sc.id ? null : sc.id)}
                last={i === schedules.length - 1}
              />
              {open === sc.id && sc.state === 'active' ? (
                <View style={s.panel}>
                  <Text style={s.panelNote}>
                    Stopping the series closes the times nobody has booked. Anyone already booked in
                    keeps their viewing — call those off one at a time, so they are told.
                  </Text>
                  <Button
                    label="Stop this series from today"
                    tone="danger"
                    disabled={busy}
                    onPress={() => run(() => endSeries(sc.id, today()))}
                  />
                </View>
              ) : null}
            </View>
          ))}
          {!loading && !error && !schedules.length ? (
            <Text style={s.empty}>No recurring times yet.</Text>
          ) : null}
        </Card>

        {adding ? (
          <Card>
            <Text style={s.formTitle}>Add a recurring time</Text>
            <Text style={s.fieldLabel}>Repeats on</Text>
            <View style={s.days}>
              {DAY_INITIALS.map((d, i) => (
                <Pressable
                  key={i}
                  accessibilityRole="checkbox"
                  accessibilityLabel={`Repeats on ${['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'][i]}`}
                  accessibilityState={{ checked: days.includes(i) }}
                  onPress={() => toggle(i)}
                  style={[s.day, days.includes(i) && s.dayOn]}
                >
                  <Text style={[s.dayText, days.includes(i) && s.dayTextOn]}>{d}</Text>
                </Pressable>
              ))}
            </View>
            <Field label="Starts at" value={time} onChange={setTime} placeholder="10:00" />
            <Field label="How long, in minutes" value={mins} onChange={setMins} keyboardType="numeric" />
            <Field label="How many people at once" value={people} onChange={setPeople} keyboardType="numeric" />
            <Field label="Meeting point" value={point} onChange={setPoint} placeholder="At the lobby desk" autoCapitalize="sentences" />
            <Field label="From" value={from} onChange={setFrom} placeholder="YYYY-MM-DD" />
            <Button label={busy ? 'Publishing…' : 'Publish these times'} disabled={busy || !days.length} onPress={publish} />
            <Button label="Cancel" tone="ghost" onPress={() => setAdding(false)} />
          </Card>
        ) : (
          <Button label="Add a recurring time" onPress={() => { setAdding(true); setOpen(null); }} style={{ marginHorizontal: space(4) }} />
        )}

        <SectionTitle>Viewings ahead</SectionTitle>
        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          {slots.map((v, i) => {
            const booked = v.booked ?? 0;
            const zone = v.zone || DEFAULT_ZONE;
            const bookable = (v.state ?? 'open') === 'open';
            return (
              <View key={v.id}>
                <ListRow
                  title={fmtOccurrence(v.starts_at, zone)}
                  subtitle={v.meeting_point || 'Meeting point not set'}
                  meta={booked ? `${booked} booked in` : bookable ? `${v.remaining ?? 0} places left` : undefined}
                  right={<StatusPill text={v.state ?? 'open'} tone={slotTone[v.state ?? 'open'] ?? 'neutral'} />}
                  onPress={() => { setOpen(open === v.id ? null : (v.id ?? null)); setMoveTo(''); }}
                  last={i === slots.length - 1}
                />
                {open && open === v.id ? (
                  <View style={s.panel}>
                    {booked ? (
                      <Text style={s.panelWarn}>
                        {booked === 1 ? '1 person is' : `${booked} people are`} booked into this time.
                        They will be told.
                      </Text>
                    ) : null}
                    <Field
                      label={`Move it to (${zone.split('/').pop()?.replace(/_/g, ' ')} time)`}
                      value={moveTo}
                      onChange={setMoveTo}
                      placeholder="14:00"
                    />
                    <Button
                      label="Move this viewing"
                      disabled={busy || !moveTo || !bookable}
                      onPress={() => move(v.id!, v.starts_at, zone)}
                    />
                    <Button
                      label="Call off this viewing"
                      tone="danger"
                      disabled={busy}
                      onPress={() => run(() => cancelViewing(v.id!))}
                    />
                  </View>
                ) : null}
              </View>
            );
          })}
          {!loading && !error && !slots.length ? (
            <Text style={s.empty}>Nothing to come. Publish a recurring time and the dates appear here.</Text>
          ) : null}
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  empty: { ...font.body, color: color.inkSoft, paddingVertical: space(6), textAlign: 'center' },
  failed: { ...font.body, color: color.negative, marginHorizontal: space(4), marginTop: space(3) },
  formTitle: { ...font.h3, color: color.ink, marginBottom: space(3) },
  fieldLabel: { ...font.label, color: color.inkSoft, marginBottom: space(2) },
  days: { flexDirection: 'row', gap: 8, marginBottom: space(4) },
  day: {
    width: 44, height: 44, borderRadius: 22, alignItems: 'center', justifyContent: 'center',
    borderWidth: 1, borderColor: color.line,
  },
  dayOn: { backgroundColor: color.accent, borderColor: color.accent },
  dayText: { ...font.body, color: color.ink },
  dayTextOn: { color: '#FFF' },
  panel: { paddingVertical: space(3), gap: space(2) },
  panelNote: { ...font.small, color: color.inkSoft, marginBottom: space(2) },
  panelWarn: { ...font.small, color: color.negative, marginBottom: space(2) },
});

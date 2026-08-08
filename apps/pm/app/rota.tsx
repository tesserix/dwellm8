import React, { useState } from 'react';
import { View, Text, StyleSheet, ActivityIndicator } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import {
  BackHeader, Button, Card, Screen, Field, SwitchRow, ErrorState, Toast,
  color, font, space, useBack,
} from '@dwellm8/mobile-shared';
import { useRota } from '../src/data/rota';

/**
 * One colleague's working week (#353).
 *
 * A rota is edited as a week: the seven days are always on screen, and what is
 * saved is what the week has become — a day switched off is a day that goes.
 */

export default function Rota() {
  const params = useLocalSearchParams<{ id?: string; name?: string }>();
  const goBack = useBack('/team');
  const rota = useRota(params.id ?? '');

  const [refused, setRefused] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const save = async () => {
    try {
      await rota.save();
      setRefused(null);
      setToast('The week is saved');
      setTimeout(() => setToast(null), 2600);
    } catch (err) {
      setRefused((err as Error).message);
    }
  };

  const hours = Math.round(rota.hours * 10) / 10;

  return (
    <>
      <BackHeader
        title="Working hours"
        subtitle={params.name ?? 'This colleague'}
        onBack={goBack}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}
        {rota.loading ? <View style={s.waiting}><ActivityIndicator /></View> : null}
        {rota.error ? <ErrorState error={rota.error} onRetry={rota.reload} /> : null}

        {!rota.loading && !rota.error ? (
          <>
            <Card>
              <Text style={s.h}>{`${hours} hours a week`}</Text>
              <Text style={s.sub}>Switch a day on to say when it is worked.</Text>
            </Card>

            {refused ? <Text style={s.refused} accessibilityRole="alert">{refused}</Text> : null}

            <Card>
              {rota.week.map((d, i) => (
                <View key={d.weekday}>
                  <SwitchRow
                    label={d.day}
                    value={d.working}
                    onChange={() => rota.toggle(d.weekday)}
                    last={!d.working && i === rota.week.length - 1}
                  />
                  {d.working ? (
                    <View style={s.hours}>
                      <View style={{ flex: 1 }}>
                        <Field
                          label={`${d.day} starts at`}
                          value={d.starts_at}
                          onChange={(v) => rota.setHours(d.weekday, v, d.ends_at)}
                          placeholder="09:00"
                        />
                      </View>
                      <View style={{ flex: 1 }}>
                        <Field
                          label={`${d.day} ends at`}
                          value={d.ends_at}
                          onChange={(v) => rota.setHours(d.weekday, d.starts_at, v)}
                          placeholder="18:00"
                        />
                      </View>
                    </View>
                  ) : null}
                </View>
              ))}
              <Text style={s.note}>
                Cover that runs past midnight is two shifts, one on each day — which is also how
                it is paid.
              </Text>
            </Card>

            <View style={s.actions}>
              <Button label="Save the week" onPress={save} style={{ flex: 1 }} />
            </View>
          </>
        ) : null}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  waiting: { paddingVertical: space(8), alignItems: 'center' },
  h: { ...font.h3, color: color.inkStrong },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  note: { ...font.small, color: color.inkFaint, marginTop: space(3), lineHeight: 18 },
  hours: { flexDirection: 'row', gap: 12, marginTop: space(3) },
  refused: {
    ...font.small, color: color.negative,
    marginHorizontal: space(4), marginTop: space(4), lineHeight: 18,
  },
  actions: { flexDirection: 'row', marginHorizontal: space(4), marginTop: space(4) },
});

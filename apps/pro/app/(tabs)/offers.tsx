import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Card, Screen, Button, StatusPill, Toast, KeyValue,
  Segmented, ListRow, MapPinIcon, ClockIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import type { Tone } from '@dwellm8/mobile-shared';
import { jobs } from '../../src/data/mock';
import { DemoNotice } from '../../src/components/DemoNotice';

/**
 * Offers and the job history.
 *
 * An offer states the pay, the distance and the window before it is accepted —
 * a technician should never discover the terms on arrival.
 */

const stateTone: Record<string, Tone> = {
  Offered: 'violet', Accepted: 'blue', Travelling: 'amber', 'On site': 'green',
  'Awaiting parts': 'amber', Completed: 'green', Paid: 'neutral',
};

export default function Offers() {
  const router = useRouter();
  const [tab, setTab] = useState('Offers');
  const [decided, setDecided] = useState<Record<string, string>>({});
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2400);
  };

  const offers = jobs.filter((j) => j.state === 'Offered');
  const history = jobs.filter((j) => j.state === 'Completed' || j.state === 'Paid');

  return (
    <>
      <AppHeader title="Offers" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <DemoNotice />
        {toast ? <Toast text={toast} /> : null}

        <View style={{ marginTop: space(3), marginBottom: space(2) }}>
          <Segmented items={['Offers', 'History']} value={tab} onChange={setTab} />
        </View>

        {tab === 'Offers' ? (
          <>
            {offers.map((j) => {
              const decision = decided[j.id];
              return (
                <Card key={j.id}>
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
                    <StatusPill text={j.priority} tone={j.priority === 'Emergency' ? 'red' : 'amber'} />
                    <View style={{ flex: 1 }} />
                    <Text style={s.pay}>{inr(j.payPaise, { noPaise: true })}</Text>
                  </View>
                  <Text style={s.title}>{j.title}</Text>
                  <Text style={s.unit}>{j.unit}</Text>

                  <View style={s.metaRow}>
                    <ClockIcon size={17} c={color.inkSoft} />
                    <Text style={s.meta}>{j.window}</Text>
                  </View>
                  <View style={s.metaRow}>
                    <MapPinIcon size={17} c={color.inkSoft} />
                    <Text style={s.meta}>{j.locality} · {j.distanceKm} km from your last job</Text>
                  </View>

                  <View style={{ marginTop: space(3) }}>
                    <KeyValue k="Category" v={j.category} />
                    <KeyValue k="Access" v={j.access} last />
                  </View>

                  {decision ? (
                    <View style={{ marginTop: space(4) }}>
                      <StatusPill text={decision} tone={decision === 'Accepted' ? 'green' : 'neutral'} />
                    </View>
                  ) : (
                    <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
                      <Button
                        label="Decline"
                        tone="secondary"
                        onPress={() => { setDecided((d) => ({ ...d, [j.id]: 'Declined' })); say('Offer declined — it goes back to the panel'); }}
                        style={{ flex: 1 }}
                      />
                      <Button
                        label="Accept"
                        onPress={() => { setDecided((d) => ({ ...d, [j.id]: 'Accepted' })); say('Accepted — the tenant has been told you are coming'); }}
                        style={{ flex: 1.4 }}
                      />
                    </View>
                  )}
                  <Text style={s.expiry}>Offer expires in 12 minutes, then it goes to the next technician.</Text>
                </Card>
              );
            })}
            {!offers.length ? <Card><Text style={s.empty}>No offers right now.</Text></Card> : null}
          </>
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4) }}>
            {history.map((j, i) => (
              <ListRow
                key={j.id}
                title={j.title}
                subtitle={`${j.unit} · ${j.window}`}
                meta={`${j.photosAfter} photos · ${inr(j.payPaise, { noPaise: true })}`}
                right={<StatusPill text={j.state} tone={stateTone[j.state]} />}
                onPress={() => router.push(`/job?id=${j.id}`)}
                last={i === history.length - 1}
              />
            ))}
          </Card>
        )}
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  pay: { ...font.h2, color: color.positive },
  title: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  unit: { ...font.body, color: color.inkSoft, marginTop: 3 },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginTop: 8 },
  meta: { ...font.body, color: color.ink, flex: 1 },
  expiry: { ...font.small, color: '#B0731C', marginTop: space(3) },
  empty: { ...font.body, color: color.inkSoft, textAlign: 'center', paddingVertical: space(5) },
});

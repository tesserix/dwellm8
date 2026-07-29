import React, { useState } from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, KeyValue, StatusPill, Button, Metric, ListRow,
  Toast, ProgressBar, Timeline, ActionBar,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { BOOST_PAISE, LISTING_DAYS, listings, myListing } from '../src/data/mock';

/**
 * Your listing's performance, and the two decisions it ever needs: promote it,
 * or take it down. Attendance is separated from registrations, because the gap
 * between them is the number a lister actually wants.
 */

export default function Manage() {
  const router = useRouter();
  const [boosted, setBoosted] = useState(myListing.boosted);
  const [live, setLive] = useState(true);
  const [toast, setToast] = useState<string | null>(null);
  const l = listings.find((x) => x.id === myListing.id)!;

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2800);
  };

  const used = LISTING_DAYS - myListing.daysLeft;

  return (
    <>
      <BackHeader title="Your listing" subtitle={myListing.title} onBack={() => router.back()} />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={live ? 'Live' : 'Taken down'} tone={live ? 'green' : 'neutral'} dot />
            {boosted ? <StatusPill text="Promoted" tone="violet" /> : null}
            <View style={{ flex: 1 }} />
            <Text style={s.days}>{myListing.daysLeft} days left</Text>
          </View>
          <ProgressBar pct={(used / LISTING_DAYS) * 100} tint={myListing.daysLeft < 14 ? color.negative : color.accent} />
          <Text style={s.sub}>Day {used} of {LISTING_DAYS} · posted {myListing.postedOn}</Text>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Metric value={myListing.views.toLocaleString('en-IN')} label="views" tone="blue" />
            <Metric value={String(myListing.shortlists)} label="shortlists" tone="violet" />
            <Metric value={String(myListing.enquiries)} label="enquiries" tone="green" />
          </View>
        </Card>

        <Card>
          <Text style={s.h}>Who came to see it</Text>
          <KeyValue k="Registered through the app" v={`${myListing.inspectionRegistered} people`} />
          <KeyValue k="Checked in with the QR" v={myListing.inspectionAttended ? String(myListing.inspectionAttended) : 'Inspection not held yet'} />
          <KeyValue k="No-shows" v={myListing.inspectionAttended ? String(myListing.inspectionRegistered - myListing.inspectionAttended) : '—'} tone="amber" />
          <KeyValue k="Every visitor is" v="A registered, contactable user" tone="green" last />
          <Text style={s.note}>
            You can message anyone who checked in, without either of you handing over a phone
            number. Nobody can attend anonymously and then disappear.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>Promotion</Text>
          <Text style={s.body}>
            {boosted
              ? 'Running for 14 days. You keep the badge that says so — seekers are told which listings paid for position.'
              : `${inr(BOOST_PAISE, { noPaise: true })} puts you at the top of matching searches for 14 days. On comparable Baner listings that has meant ${myListing.boostUplift}.`}
          </Text>
          <Button
            label={boosted ? 'Stop promoting' : `Promote for ${inr(BOOST_PAISE, { noPaise: true })}`}
            tone={boosted ? 'secondary' : 'primary'}
            onPress={() => { setBoosted(!boosted); say(boosted ? 'Promotion stopped' : 'Promoted — live at the top within the hour'); }}
            style={{ marginTop: space(4) }}
          />
          <Text style={s.note}>
            Promotion never affects your verification badge, and an unverified listing cannot be
            promoted at all.
          </Text>
        </Card>

        <Card>
          <Text style={s.h}>When this expires</Text>
          <Timeline
            items={[
              { at: `Day ${LISTING_DAYS - 15}`, what: 'We ask whether it is still available' },
              { at: `Day ${LISTING_DAYS}`, what: 'It comes down automatically' },
              { at: 'After that', what: 'Re-publish in one tap — verification is already done', done: false },
            ]}
          />
        </Card>

        <Card padded={false} style={{ paddingHorizontal: space(4) }}>
          <ListRow title="Edit the listing" subtitle="Rent, availability, photographs" onPress={() => router.push('/publish')} />
          <ListRow title="View as a seeker" subtitle="Exactly what a renter sees" onPress={() => router.push(`/listing?id=${l.id}`)} />
          <ListRow title="Have Dwellm8 manage it" subtitle="2.99% at payout, nothing before" onPress={() => say('A manager will call you today')} last />
        </Card>
      </Screen>

      <ActionBar>
        <Button
          label={live ? 'Take it down' : 'Publish again'}
          tone={live ? 'secondary' : 'primary'}
          onPress={() => { setLive(!live); say(live ? 'Taken down — re-publish any time' : 'Live again'); }}
          style={{ flex: 1 }}
        />
        <Button label="Extend 90 days" onPress={() => say('Extended — verification carried over')} style={{ flex: 1 }} />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  sub: { ...font.small, color: color.inkSoft, marginTop: space(2) },
  days: { ...font.small, color: color.inkSoft, fontWeight: '700' },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
});

import React from 'react';
import { View, Text, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import {
  AppHeader, AvatarButton, Screen, Card, Button, KeyValue, ListRow, Metric,
  StatusPill, Timeline, ShieldIcon, PlusIcon, ChartIcon,
  color, font, inr, space,
} from '@dwellm8/mobile-shared';
import { BOOST_PAISE, LISTING_DAYS, myListing } from '../../src/data/mock';

/**
 * List your property.
 *
 * Free, for owners and agencies alike — the marketplace earns when a listing
 * turns into a managed tenancy, not when someone posts. What it asks for
 * instead of money is proof, which is the only thing that makes the rest of
 * the app worth trusting.
 */

export default function ListTab() {
  const router = useRouter();

  return (
    <>
      <AppHeader title="Your listings" showCaret={false} left={<AvatarButton onPress={() => router.push('/profile')} />} />
      <Screen>
        <Card>
          <StatusPill text="Free to list" tone="green" dot />
          <Text style={s.h1}>Let your flat without paying a broker</Text>
          <Text style={s.body}>
            Owners and managing agencies list on the same terms. No listing fee, no commission to
            us, and no charge to the tenant for being shown a home.
          </Text>
          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Listing runs for" v={`${LISTING_DAYS} days, then it comes down`} />
            <KeyValue k="Cost to list" v="Free" tone="green" />
            <KeyValue k="Optional promotion" v={`${inr(BOOST_PAISE, { noPaise: true })} for 14 days at the top`} />
            <KeyValue k="What we earn" v="2.99% at payout, only if you have us manage it" last />
          </View>
          <Button
            label="List a property"
            icon={<PlusIcon size={19} c="#FFF" />}
            onPress={() => router.push('/publish')}
            style={{ marginTop: space(4) }}
          />
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <ShieldIcon size={20} />
            <Text style={s.h}>What you will need</Text>
          </View>
          <Text style={s.body}>
            Nothing goes live until these are checked. It takes about a day, and it is why a seeker
            trusts what they read here.
          </Text>
          <View style={{ marginTop: space(3) }}>
            <KeyValue k="Proof of ownership" v="Khata, sale deed, or your management agreement" />
            <KeyValue k="Your identity" v="Any government ID, matched to the document" />
            <KeyValue k="The address" v="We geocode it and check it against the document" />
            <KeyValue k="Photographs" v="Taken on site — not lifted from another portal" last />
          </View>
        </Card>

        <Text style={s.section}>Live listing</Text>
        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <StatusPill text={`${myListing.daysLeft} of ${LISTING_DAYS} days left`} tone={myListing.daysLeft < 14 ? 'red' : 'blue'} />
            <View style={{ flex: 1 }} />
            {myListing.boosted ? <StatusPill text="Promoted" tone="violet" /> : null}
          </View>
          <Text style={s.h}>{myListing.title}</Text>
          <Text style={s.sub}>Posted {myListing.postedOn}</Text>

          <View style={{ flexDirection: 'row', gap: 10, marginTop: space(4) }}>
            <Metric value={myListing.views.toLocaleString('en-IN')} label="views" tone="blue" />
            <Metric value={String(myListing.shortlists)} label="shortlists" tone="violet" />
            <Metric value={String(myListing.enquiries)} label="enquiries" tone="green" />
          </View>

          <View style={{ marginTop: space(4) }}>
            <KeyValue k="Registered for inspection" v={`${myListing.inspectionRegistered} people`} />
            <KeyValue k="Actually attended" v={myListing.inspectionAttended ? String(myListing.inspectionAttended) : 'Inspection not held yet'} last />
          </View>
          <Text style={s.note}>
            Everyone who scans the QR at your door is a registered Dwellm8 user, so you know who came
            and can message them afterwards. No walk-in with a fake number.
          </Text>

          <Button label="Manage this listing" onPress={() => router.push('/manage')} style={{ marginTop: space(4) }} />
        </Card>

        <Card>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
            <ChartIcon size={20} />
            <Text style={s.h}>Why listings expire</Text>
          </View>
          <Timeline
            items={[
              { at: 'Day 1', what: 'Verified and live, appearing in search and alerts' },
              { at: 'Day 75', what: 'We ask whether it is still available' },
              { at: `Day ${LISTING_DAYS}`, what: 'It comes down automatically' },
              { at: 'Any time', what: 'Re-publish in one tap if it is still free', done: false },
            ]}
          />
          <Text style={s.note}>
            Indian portals are full of flats that were let last year. A hard 90-day life is the
            simplest fix: what you are reading is either current, or it is not here.
          </Text>
        </Card>
      </Screen>
    </>
  );
}

const s = StyleSheet.create({
  h1: { ...font.h2, color: color.inkStrong, marginTop: space(3) },
  h: { ...font.h3, color: color.inkStrong, marginTop: space(3) },
  sub: { ...font.small, color: color.inkSoft, marginTop: 3 },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  note: { ...font.small, color: color.inkSoft, marginTop: space(3), lineHeight: 18 },
  section: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginTop: space(5), marginBottom: space(3) },
});

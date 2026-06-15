package bot

// Hard-coded user-facing strings from modules/settings.py and
// modules/my_links.py (the templated ones live in Texts/settings/* and
// Texts/mylinks.txt and are served by internal/texts). Reproduced byte-for-byte.
const (
	// settings.py
	txtSettingsTagPlaceholder  = "[تگی ثبت نکردی]"
	txtWPPDefault              = "حالت پیشفرض"
	txtWPPDisabled             = "غیرفعال"
	txtStateActive             = "فعال"
	txtStateInactive           = "غیرفعال"
	txtWPPAnswerActivate       = "پیشنمایش به حالتِ پیشفرض تبدیل شد"
	txtWPPAnswerDeactivate     = "پیشنمایش غیرفعال شد"
	txtWarningAnswerActivate   = "اخطار فعال شد"
	txtWarningAnswerDeactivate = "اخطار غیرفعال شد"
	txtSeenAnswerActivate      = "آپشن سین زدن فعال شد"
	txtSeenAnswerDeactivate    = "آپشن سین زدن غیرفعال شد"
	txtTagRemoved              = "تگ دلخواهت با موفقیت پاک شد"
	txtUnblockAllDone          = "همه با موفقیت آنبلاک شدن"
	txtUnblockAllConfirm       = "اگه میخوای همه رو آنبلاک کنی و مطمئنی، دکمه ی زیر رو بزن"
	btnUnblockAllSure          = "مطمئنم، بزن همه رو آنبلاک کن"
	txtUnblockMeIntro          = "این لینک رو بفرس به یکی که بلاکت کرده. وقتی که بزنه روش آنبلاک میشی:"
	txtSettingsCancelAll       = "در حال تغییر تنظیماتت بودی پس کنسلش کردم. دوباره امتحان کن"
	txtCancelledAllGeneric     = "هرچی که بود کنسل شد"

	// my_links.py
	txtMyLinksPromo         = "<blockquote>راستی یادت نره به کانالمون سر بزنی:\n@chevalet_studio</blockquote>"
	txtAddedNewLink         = "added a new link"
	txtRmConfirmTail        = "\n\nمطمئنی از حذفش؟ قابل برگشت نیست"
	btnRmSure               = "✅ آره مطمئنم"
	btnRmNo                 = "نههه پاک نکن"
	txtRmDeletedRegen       = "با موفقیت حذف شد ولی چون فقط یک لینک داشتی، بجاش یکی دیگه تولید شد"
	txtRmDeleted            = "با موفقیت حذف شد."
	txtRmCancelled          = "کنسل شد"
	txtChChooseTail         = "\n\nانتخاب کن"
	txtCidCharErr           = "فقط حروف کوچیک و بزرگ انگلیسی، اعداد، آندرلاین و خط تیره مجازه. دوباره امتحان کن"
	txtCidTaken             = "این آیدی برداشته شده. آیدی دیگه ای بفرس"
	txtCidChanged           = "با موفقیت تغییر یافت"
	txtCidIntegrity         = "ظاهرا یکی زودتر این آیدی رو برداشت. دوباره امتحان کن"
	txtChangingCidCancelled = "در حال تغییر آیدی بودی پس کنسلش کردم. دوباره بفرست"
)

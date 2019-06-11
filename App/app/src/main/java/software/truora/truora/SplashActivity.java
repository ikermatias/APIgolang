package software.truora.truora;

import android.content.Intent;
import android.graphics.Typeface;
import android.os.Bundle;
import android.support.v7.app.AppCompatActivity;
import android.view.View;
import android.widget.ImageView;
import android.widget.TextView;

import java.util.Timer;
import java.util.TimerTask;

public class SplashActivity extends AppCompatActivity {

    Timer mTimer;
    int startingMillis = 2000;
    ImageView logoimg;
    TextView logotxt;
    TextView logoLetra;
    TextView powered;
    //TextView logoDemand;
    View grupo;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_splash);

        //logoimg = findViewById(R.id.logoimg);
        //logotxt = findViewById(R.id.logotxt);
        //grupo = findViewById(R.id.group);
        //logoDemand = findViewById(R.id.demand);
        logoLetra = findViewById(R.id.letraLogo);
        powered = findViewById(R.id.powered);

        //Typeface typeface = Typeface.createFromAsset(getAssets(), "font/Barlow-SemiBold.ttf");
        //logoDemand.setTypeface(typeface);
        //logoLetra.setTypeface(typeface);
        //logoSupply.setTypeface(typeface);
        startTimer();
    }

    public void startTimer(){
        mTimer = new Timer();
        mTimer.schedule(new TimerTask() {
            @Override
            public void run() {

                logoLetra.animate().alpha(0).translationY(-2f).setDuration(500).withEndAction(new Runnable() {
                    @Override
                    public void run() {
                        logoLetra.animate().translationY(2f).setDuration(500).alpha(0).withEndAction(new Runnable() {
                            @Override
                            public void run() {
                                startActivity(new Intent(getApplicationContext(), MainActivity.class));
                            }
                        });
                    }
                });
            }
        },startingMillis);
    }
}
